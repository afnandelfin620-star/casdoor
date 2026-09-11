package conf

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	cftpConfig *CftpConfig
)

type CftpConfig struct {
	Database      string `json:"Database"`
	DBUser        string `json:"DBUser"`
	DBPassword    string `json:"DBPassword"`
	RedisPassword string `json:"RedisPassword"`
}

// ConfigurePostgresSSL configures PostgreSQL connection SSL parameters.
// If PG_SSLMODE is explicitly set, it respects that setting.
// Otherwise, if TLS_DIR contains ca.crt, it sets sslmode=verify-full with sslrootcert.
// If ca.crt is not found, it defaults to sslmode=disable.
func ConfigurePostgresSSL(q url.Values) {
	if customMode := strings.TrimSpace(os.Getenv("PG_SSLMODE")); customMode != "" {
		q.Set("sslmode", customMode)
		return
	}

	tlsDir := strings.TrimSpace(os.Getenv("TLS_DIR"))
	if tlsDir != "" {
		caFile := filepath.Join(tlsDir, "ca.crt")
		if _, err := os.Stat(caFile); err == nil {
			q.Set("sslmode", "verify-full")
			q.Set("sslrootcert", caFile)
			return
		}
	}

	q.Set("sslmode", "disable")
}

func (c *CftpConfig) GetDatabaseDSN() string {
	pgAddress := GetEndpointAddress("POSTGRES_ADDR", "pgbouncer-external", "6432")

	q := url.Values{}
	ConfigurePostgresSSL(q)

	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.DBUser, c.DBPassword),
		Host:     pgAddress,
		Path:     "/" + c.Database,
		RawQuery: q.Encode(),
	}

	return u.String()
}

func (c *CftpConfig) checkRequiredFields() error {
	fields := []struct {
		name  string
		value string
	}{
		{"Database", c.Database},
		{"DBUser", c.DBUser},
		{"DBPassword", c.DBPassword},
	}

	for _, field := range fields {
		if field.value == "" {
			return fmt.Errorf("required field %s is missing", field.name)
		}
	}

	return nil
}

// IsRunningInK8s checks whether the service is running inside a Kubernetes cluster.
func IsRunningInK8s() bool {
	_, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount")
	return err == nil
}

func GetNamespace() (string, error) {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func GetEndpointAddress(envName, svcName, port string) string {
	endpoint := os.Getenv(envName)
	if endpoint == "" {
		if IsRunningInK8s() {
			namespace, err := GetNamespace()
			if err != nil {
				namespace = "default"
			}
			endpoint = fmt.Sprintf("%s.%s.svc.cluster.local:%s", svcName, namespace, port)
		} else {
			endpoint = fmt.Sprintf("%s:%s", svcName, port)
		}
	}
	return endpoint
}

func getCfgServerTransportCreds() credentials.TransportCredentials {
	tlsDir := strings.TrimSpace(os.Getenv("TLS_DIR"))
	if tlsDir == "" {
		return insecure.NewCredentials()
	}

	caFile := filepath.Join(tlsDir, "ca.crt")
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("gRPC client: CA cert not found, using plaintext mode", "ca_file", caFile)
		} else {
			slog.Warn("gRPC client: failed to read CA cert, falling back to plaintext", "ca_file", caFile, "error", err)
		}
		return insecure.NewCredentials()
	}

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caPEM); !ok {
		slog.Warn("gRPC client: failed to append CA cert, falling back to plaintext", "ca_file", caFile)
		return insecure.NewCredentials()
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}
	return credentials.NewTLS(tlsConfig)
}

func LoadCftpConfig() error {
	address := GetEndpointAddress("CFGSERVER_ADDR", "cfgserver", "50051")
	transportCreds := getCfgServerTransportCreds()

	var conn *grpc.ClientConn
	var err error

	for i := 0; i < 5; i++ {
		conn, err = grpc.NewClient(address, grpc.WithTransportCredentials(transportCreds))
		if err == nil {
			break
		}
		slog.Error("Failed to connect to cfgserver", "attempt", i+1, "error", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("could not connect to cfgserver: %w", err)
	}
	defer conn.Close()

	client := NewConfigServiceClient(conn)

	var resp *GetConfigResponse
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		resp, err = client.GetSystemConfig(ctx, &GetConfigRequest{
			SystemName: "casdoor,passwords",
		})
		cancel()
		if err == nil {
			break
		}
		slog.Error("Failed to get config from cfgserver", "attempt", i+1, "error", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to get config from cfgserver: %w", err)
	}

	var raw struct {
		Casdoor struct {
			Database      string `json:"Database"`
			DBUser        string `json:"DBUser"`
			DBPassword    string `json:"DBPassword"`
			RedisPassword string `json:"RedisPassword"`
		} `json:"casdoor"`
		Passwords struct {
			DBPassword    string `json:"DBPassword"`
			RedisPassword string `json:"RedisPassword"`
		} `json:"passwords"`
	}

	c := &CftpConfig{}
	if err = json.Unmarshal([]byte(resp.ConfigJson), &raw); err == nil && raw.Casdoor.Database != "" {
		c.Database = raw.Casdoor.Database
		c.DBUser = raw.Casdoor.DBUser
		c.DBPassword = raw.Casdoor.DBPassword
		c.RedisPassword = raw.Casdoor.RedisPassword

		if c.DBPassword == "" {
			c.DBPassword = raw.Passwords.DBPassword
		}
		if c.RedisPassword == "" {
			c.RedisPassword = raw.Passwords.RedisPassword
		}
	} else {
		// Fallback: unmarshal directly if response is a single object without system wrappers
		if unmarshalErr := json.Unmarshal([]byte(resp.ConfigJson), c); unmarshalErr != nil {
			return fmt.Errorf("failed to unmarshal secret config: %w", unmarshalErr)
		}
	}

	if err = c.checkRequiredFields(); err != nil {
		return err
	}

	cftpConfig = c
	return nil
}
