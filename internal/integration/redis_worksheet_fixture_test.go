//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/service"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const redisFixtureImage = "redis:8.10.3-alpine@sha256:7958cfbb9d152f87dadfbc89f1b0b15938f0b81ff8982feb327476f4911935c3"

type redisFixtureConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (c redisFixtureConnection) command(args ...string) (string, error) {
	var request bytes.Buffer
	fmt.Fprintf(&request, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&request, "$%d\r\n%s\r\n", len(arg), arg)
	}
	if _, err := c.Write(request.Bytes()); err != nil {
		return "", err
	}
	return c.line()
}

func (c redisFixtureConnection) line() (string, error) {
	line, err := c.reader.ReadSlice('\n')
	if err != nil || !bytes.HasSuffix(line, []byte("\r\n")) {
		return "", errors.New("incomplete Redis frame")
	}
	return string(line[:len(line)-2]), nil
}

func (c redisFixtureConnection) expect(expected string, args ...string) error {
	line, err := c.command(args...)
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "-NOPERM ") {
		return service.ErrQueryNotAllowed
	}
	if line != expected {
		return errors.New("unexpected Redis frame")
	}
	return nil
}

func redisFixtureTLS(t *testing.T) ([]byte, []byte, *tls.Config) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("Redis fixture certificate could not be trusted")
	}
	return certPEM, keyPEM, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "127.0.0.1", RootCAs: roots}
}

func dialRedisFixture(ctx context.Context, address, user, password string, tlsConfig *tls.Config) (redisFixtureConnection, error) {
	dialer := tls.Dialer{Config: tlsConfig}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return redisFixtureConnection{}, err
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return redisFixtureConnection{}, err
	}
	client := redisFixtureConnection{Conn: conn, reader: bufio.NewReaderSize(conn, 2048)}
	if err := client.expect("+OK", "AUTH", user, password); err != nil {
		conn.Close()
		return redisFixtureConnection{}, err
	}
	return client, nil
}

func startRedisFixture(t *testing.T) (string, string, string, string, *tls.Config) {
	t.Helper()
	cert, key, tlsConfig := redisFixtureTLS(t)
	adminPassword := hex.EncodeToString(randomRedisFixtureSecret(t))
	readerPassword := hex.EncodeToString(randomRedisFixtureSecret(t))
	acl := fmt.Sprintf("user default reset off\nuser fixture on #%x ~* +@all\nuser reader on #%x %%R~demo:order:* +multi +exists +getrange +exec +watch +hstrlen +hget\n",
		sha256.Sum256([]byte(adminPassword)), sha256.Sum256([]byte(readerPassword)))
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	redis, err := testcontainers.Run(ctx, redisFixtureImage,
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithHostConfigModifier(func(config *container.HostConfig) {
			config.PortBindings = network.PortMap{network.MustParsePort("6379/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1")}}}
			config.Memory = 128 << 20
		}),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{Reader: bytes.NewReader(cert), ContainerFilePath: "/tmp/redis-lab.crt", FileMode: 0444},
			testcontainers.ContainerFile{Reader: bytes.NewReader(key), ContainerFilePath: "/tmp/redis-lab.key", FileMode: 0444},
			testcontainers.ContainerFile{Reader: strings.NewReader(acl), ContainerFilePath: "/tmp/redis-lab.acl", FileMode: 0444},
		),
		testcontainers.WithCmd("redis-server", "--port", "0", "--tls-port", "6379", "--tls-cert-file", "/tmp/redis-lab.crt",
			"--tls-key-file", "/tmp/redis-lab.key", "--tls-ca-cert-file", "/tmp/redis-lab.crt", "--tls-auth-clients", "no",
			"--aclfile", "/tmp/redis-lab.acl", "--save", "", "--appendonly", "no"),
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections"), wait.ForListeningPort("6379/tcp").SkipInternalCheck()),
	)
	testcontainers.CleanupContainer(t, redis)
	if redis != nil {
		t.Cleanup(func() {
			if !t.Failed() {
				return
			}
			logCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			logs, err := redis.Logs(logCtx)
			if err == nil {
				defer logs.Close()
				lines, err := io.ReadAll(io.LimitReader(logs, 8192))
				if err == nil {
					t.Logf("Redis fixture logs: %s", lines)
				}
			}
		})
	}
	if err != nil {
		t.Fatalf("start Redis fixture: %v", err)
	}
	port, err := redis.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return net.JoinHostPort("127.0.0.1", port.Port()), adminPassword, readerPassword, string(cert), tlsConfig
}

func randomRedisFixtureSecret(t *testing.T) []byte {
	t.Helper()
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return secret
}
