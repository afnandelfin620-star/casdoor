package conf

import (
	"context"
	"crypto/tls"
	"crypto/x509"    
	"encoding/json"
	"fmt"
	"log/slog"    
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

func (c* CftpConfig) GetDatabaseDSN() string {
	mySqlAddress := os.Getenv("MYSQL_ADDR")
	if mySqlAddress == "" {

		mysqlExternalServiceName := "mysql-external"
		namespace, err := GetNamespace()
		if err != nil {
			namespace = "default"
		}

		mysqlPort := "3306"
		mySqlAddress = fmt.Sprintf("%s.%s.svc.cluster.local:%s", mysqlExternalServiceName, namespace, mysqlPort)
	}

    databaseDSN := fmt.Sprintf("%s:%s@tcp(%s)/",
		c.DBUser,
		c.DBPassword,
		mySqlAddress)

    return databaseDSN
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

// 判断是否运行在 K8s 环境
func IsRunningInK8s() bool {
	// 方式 A：检查 K8s 默认挂载的 ServiceAccount 路径
	_, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount")
	return err == nil
}

func GetNamespace() (string, error) {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", err
	}
	// 读取的内容末尾通常有换行符，需要去除
	return strings.TrimSpace(string(data)), nil
}

func getCfgServerTransportCreds() credentials.TransportCredentials {
	tlsDir := strings.TrimSpace(os.Getenv("TLS_DIR"))
	if tlsDir == "" {
		return insecure.NewCredentials()
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	caFile := filepath.Join(tlsDir, "ca.crt")
	if caPEM, err := os.ReadFile(caFile); err == nil {
		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM(caPEM); !ok {
			slog.Warn("gRPC: failed to append CA cert", "ca_file", caFile)
			return insecure.NewCredentials()
		}

		tlsConfig.RootCAs = pool
	} else {
		slog.Warn("gRPC: load ca faild", "ca_file", caFile, "error", err)
		return insecure.NewCredentials()
	}

	return credentials.NewTLS(tlsConfig)
}

func LoadCftpConfig() error {
	address := os.Getenv("CFGSERVER_ADDR")
	if address == "" {
		port := "50051" // 兜底默认端口
		namespace, err := GetNamespace()
		if err != nil {
			namespace = "default"
		}
		hostName := "cfgserver." + namespace + ".svc.cluster.local"
		address = fmt.Sprintf("%s:%s", hostName, port) // 使用HTTP，不要使用HTTPS
	}

	transportCreds := getCfgServerTransportCreds()

	// 1. 建立 gRPC 连接 (使用新版 WithTransportCredentials)
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(transportCreds))
	if err != nil {
		return fmt.Errorf("could not connect to cfgserver: %v", err)
	}
	defer conn.Close()

	client := NewConfigServiceClient(conn)

	// 2. 设置超时 context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 3. 调用 gRPC 获取配置
	resp, err := client.GetSystemConfig(ctx, &GetConfigRequest{
		SystemName: "casdoor", // 对应你 cfgserver 中的配置标识
	})
	if err != nil {
		return fmt.Errorf("failed to get config from cfgserver: %v", err)
	}

	c := &CftpConfig{}

	// 4. 解析涉密配置 JSON
	err = json.Unmarshal([]byte(resp.ConfigJson), &c)
	if err != nil {
		return fmt.Errorf("failed to unmarshal secret config: %v", err)
	}

	// 校验核心字段
	if c.Database == "" || c.DBUser == "" || c.DBPassword == "" {
		return fmt.Errorf("database config is empty; please check cfgserver")
	}
    
	if err = c.checkRequiredFields(); err != nil {
		return err
	}

    cftpConfig = c

	return nil
}
