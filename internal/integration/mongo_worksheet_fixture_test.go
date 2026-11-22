//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const mongoWorksheetImage = "mongo:8.0.38@sha256:479e85ced54ffb8a40dade1dcde2da2a9ae858515b9415e9cc54bfb8f0575d01"

// mongoFixtureTLS mints a self-signed server certificate for 127.0.0.1 and the
// client-side trust config. The returned PEM strings are the CA (cert only)
// and the combined cert+key the server loads.
func mongoFixtureTLS(t *testing.T) (caPEM, serverPEM string, clientTLS *tls.Config) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cert := x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "controlhub-mongo-fixture"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(2 * time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("Mongo fixture certificate could not be trusted")
	}
	return string(certPEM), string(certPEM) + string(keyPEM), &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "127.0.0.1", RootCAs: roots}
}

// startMongoWorksheetFixture runs a single-node replica set with auth and
// requireTLS. The keyfile is generated inside the container and the TLS PEM is
// injected through the environment so the startup wrapper owns file creation
// before mongod launches. GLIBC_TUNABLES works around the tcmalloc/rseq
// startup refusal on Docker Desktop VM kernels 6.19–7.0.13 (SERVER-121912).
func startMongoWorksheetFixture(t *testing.T) (address, rootPassword, caPEM string, tlsConfig *tls.Config) {
	t.Helper()
	var serverPEM string
	caPEM, serverPEM, tlsConfig = mongoFixtureTLS(t)
	rootPassword = mongoRandomSecret(t, 12)
	wrapper := strings.Join([]string{
		"set -e",
		"head -c 600 /dev/urandom | base64 > /data/keyfile",
		"chown mongodb:mongodb /data/keyfile",
		"chmod 0400 /data/keyfile",
		`printf '%s' "$MONGO_TLS_PEM" > /data/tls.pem`,
		`printf '%s' "$MONGO_CA_PEM" > /data/ca.pem`,
		"chown mongodb:mongodb /data/tls.pem /data/ca.pem",
		"chmod 0400 /data/tls.pem /data/ca.pem",
		"export GLIBC_TUNABLES=glibc.pthread.rseq=1",
		"gosu mongodb mongod --replSet rs0 --bind_ip_all --keyFile /data/keyfile --tlsMode requireTLS --tlsCertificateKeyFile /data/tls.pem --tlsCAFile /data/ca.pem --tlsAllowConnectionsWithoutCertificates &",
		"until mongosh --quiet --tls --tlsAllowInvalidCertificates --eval 'db.runCommand({ping:1}).ok' >/dev/null 2>&1; do sleep 0.2; done",
		`mongosh --quiet --tls --tlsAllowInvalidCertificates --eval 'rs.initiate({_id:"rs0",members:[{_id:0,host:"localhost:27017"}]})'`,
		`until mongosh --quiet --tls --tlsAllowInvalidCertificates --eval 'db.hello().isWritablePrimary' 2>/dev/null | grep -qx true; do sleep 0.2; done`,
		`mongosh --quiet --tls --tlsAllowInvalidCertificates --eval 'db.getSiblingDB("admin").createUser({user:"root",pwd:process.env.MONGO_ROOT_PW,roles:[{role:"root",db:"admin"}]})'`,
		"wait",
	}, "\n")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	m, err := testcontainers.Run(ctx, mongoWorksheetImage,
		testcontainers.WithExposedPorts("27017/tcp"),
		testcontainers.WithHostConfigModifier(func(config *container.HostConfig) {
			config.PortBindings = network.PortMap{network.MustParsePort("27017/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1")}}}
			config.Memory = 1 << 30
		}),
		testcontainers.WithEnv(map[string]string{"MONGO_ROOT_PW": rootPassword, "MONGO_TLS_PEM": serverPEM, "MONGO_CA_PEM": caPEM}),
		testcontainers.WithEntrypoint("bash", "-c"),
		testcontainers.WithCmd(wrapper),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("27017/tcp").SkipInternalCheck()),
	)
	cancel()
	testcontainers.CleanupContainer(t, m)
	if err != nil {
		t.Fatalf("start MongoDB worksheet fixture: %v", err)
	}
	port, err := m.MappedPort(t.Context(), "27017/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return net.JoinHostPort("127.0.0.1", port.Port()), rootPassword, caPEM, tlsConfig
}

// mongoFixtureClient connects with TLS and SCRAM-SHA-256 and waits for the
// primary to accept commands.
func mongoFixtureClient(t *testing.T, address, user, password, authDB string, tlsConfig *tls.Config) *mongo.Client {
	t.Helper()
	client, err := mongo.Connect(options.Client().
		SetHosts([]string{address}).SetDirect(true).
		SetAuth(options.Credential{AuthMechanism: "SCRAM-SHA-256", Username: user, Password: password, AuthSource: authDB}).
		SetTLSConfig(tlsConfig).SetTimeout(30 * time.Second).SetServerSelectionTimeout(30 * time.Second))
	if err != nil {
		t.Fatalf("mongo fixture connect %s: %v", user, err)
	}
	pingCtx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	for {
		var hello bson.M
		err := client.Database("admin").RunCommand(pingCtx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello)
		if err == nil {
			if primary, _ := hello["isWritablePrimary"].(bool); primary {
				break
			}
		}
		select {
		case <-pingCtx.Done():
			t.Fatalf("mongo fixture %s never became primary: %v", user, err)
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client
}
