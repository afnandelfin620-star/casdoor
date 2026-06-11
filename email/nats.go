// Copyright 2024 The Casdoor Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package email

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/beego/beego/v2/core/logs"
	"github.com/casdoor/casdoor/util"
	"github.com/nats-io/nats.go"
)

var globalNatsConn *nats.Conn
var globalJetStream nats.JetStreamContext

// EmailMessage is the NATS payload published when no email provider is configured.
// The gmail microservice consumes this and sends the actual email.
type EmailMessage struct {
	MailID       string `json:"mail_id"`
	BusinessUnit string `json:"business_unit"`
	FromAddress  string   `json:"from_address"`
	FromName     string   `json:"from_name"`
	ToAddresses  []string `json:"to_addresses"`
	Subject      string   `json:"subject"`
	Content      string   `json:"content"`
}

func getNamespace() (string, error) {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func getEndpointAddress(envName, svcName, port string) string {
	endpoint := os.Getenv(envName)
	if endpoint == "" {
		endpoint = svcName
		namespace, err := getNamespace()
		if err != nil {
			namespace = "default"
		}
		endpoint = fmt.Sprintf("%s.%s.svc.cluster.local:%s", svcName, namespace, port)
	}
	return endpoint
}

func InitNatsConnection() {
	natAddress := getEndpointAddress("NATS_ADDR", "nats", "4222")

	opts := []nats.Option{
		nats.Name("casdoor"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * nats.DefaultReconnectWait),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			logs.Error("NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logs.Info("NATS reconnected to %s", nc.ConnectedUrl())
		}),
	}

	url := "nats://" + natAddress

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		logs.Warning("failed to connect to NATS (email fallback will be unavailable): %v, url: %s", err, url)
		return
	}

	js, err := nc.JetStream()
	if err != nil {
		logs.Warning("failed to get JetStream context (email fallback will be unavailable): %v", err)
		nc.Close()
		return
	}

	globalNatsConn = nc
	globalJetStream = js
	logs.Info("NATS JetStream connected for email fallback, url: %s", url)
}

func CloseNatsConnection() {
	if globalNatsConn != nil {
		globalNatsConn.Close()
		globalNatsConn = nil
		globalJetStream = nil
	}
}

// InnerNatsEmailProvider publishes email content to NATS JetStream. The gmail
// microservice consumes the messages and sends the emails. Unlike SMTP providers,
// InnerNATS has no SMTP credentials — only templates (Title, Content, Metadata).
type InnerNatsEmailProvider struct {
	businessUnit string
}

func NewInnerNatsEmailProvider(businessUnit string) *InnerNatsEmailProvider {
	return &InnerNatsEmailProvider{businessUnit: businessUnit}
}

// Send implements the EmailProvider interface. The content is already rendered
// from the provider's templates by SendVerificationCodeToEmail or SendEmail.
func (p *InnerNatsEmailProvider) Send(fromAddress, fromName string, toAddresses []string, subject, content string) error {
	if globalJetStream == nil {
		return fmt.Errorf("NATS JetStream is not available")
	}

	msg := EmailMessage{
		MailID:       util.GenerateULID(),
		BusinessUnit: p.businessUnit,
		FromAddress:  fromAddress,
		FromName:     fromName,
		ToAddresses:  toAddresses,
		Subject:      subject,
		Content:      content,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal email message: %w", err)
	}

	subjectName := os.Getenv("NATS_EMAIL_SUBJECT")
	if subjectName == "" {
		subjectName = "casdoor.email.send"
	}

	_, err = globalJetStream.Publish(subjectName, data)
	if err != nil {
		return fmt.Errorf("failed to publish email to NATS JetStream: %w", err)
	}

	return nil
}
