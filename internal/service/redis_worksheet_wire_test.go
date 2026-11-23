package service

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

func TestRedisWorksheetReplyBounds(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		value       any
		err         error
		completed   bool
	}{
		{name: "missing", reply: ":0\r\n$0\r\n\r\n", completed: true},
		{name: "empty", reply: ":1\r\n$0\r\n\r\n", value: "", completed: true},
		{name: "overflow probe", reply: ":1\r\n$1025\r\n" + strings.Repeat("x", 1025) + "\r\n", err: ErrQueryResultTooLarge, completed: true},
		{name: "unbounded declared size", reply: ":1\r\n$999999999\r\n", err: ErrQueryBackendFailure},
		{name: "missing key with a value", reply: ":0\r\n$1\r\nx\r\n", err: ErrQueryBackendFailure, completed: true},
		{name: "wrong type", reply: ":1\r\n-WRONGTYPE unexpected type\r\n", err: ErrQueryValidationFailed, completed: true},
		{name: "refused in EXEC", reply: ":1\r\n-NOPERM protected key\r\n", err: ErrQueryNotAllowed, completed: true},
		{name: "invalid UTF-8", reply: ":1\r\n$1\r\n\xff\r\n", err: ErrQueryValidationFailed, completed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := redisWorksheetWire{reader: bufio.NewReader(strings.NewReader(tc.reply))}
			remote := model.QueryExecutionRemoteUnknown
			got, err := wire.getReply(&remote)
			if got != tc.value || !errors.Is(err, tc.err) || (remote == model.QueryExecutionRemoteCompleted) != tc.completed {
				t.Fatalf("value=%v error=%v remote=%s", got, err, remote)
			}
		})
	}
}

func TestRedisWorksheetHGETReplyBounds(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		value       any
		err         error
		completed   bool
	}{
		{name: "missing field", reply: "$-1\r\n", completed: true},
		{name: "empty field", reply: "$0\r\n\r\n", value: "", completed: true},
		{name: "value", reply: "$5\r\nhello\r\n", value: "hello", completed: true},
		{name: "limit", reply: "$1024\r\n" + strings.Repeat("x", 1024) + "\r\n", value: strings.Repeat("x", 1024), completed: true},
		{name: "over-limit declared size never read", reply: "$1025\r\n", err: ErrQueryBackendFailure},
		{name: "unbounded declared size", reply: "$999999999\r\n", err: ErrQueryBackendFailure},
		{name: "non-canonical size", reply: "$007\r\n", err: ErrQueryBackendFailure},
		{name: "wrong type", reply: "-WRONGTYPE unexpected type\r\n", err: ErrQueryValidationFailed, completed: true},
		{name: "refused in EXEC", reply: "-NOPERM protected key\r\n", err: ErrQueryNotAllowed, completed: true},
		{name: "invalid UTF-8", reply: "$1\r\n\xff\r\n", err: ErrQueryValidationFailed, completed: true},
		{name: "missing CRLF", reply: "$1\r\nx..", err: ErrQueryBackendFailure},
		{name: "truncated payload", reply: "$5\r\nx", err: io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := redisWorksheetWire{reader: bufio.NewReader(strings.NewReader(tc.reply))}
			remote := model.QueryExecutionRemoteUnknown
			got, err := wire.hgetReply(&remote)
			if got != tc.value || !errors.Is(err, tc.err) || (remote == model.QueryExecutionRemoteCompleted) != tc.completed {
				t.Fatalf("value=%v error=%v remote=%s", got, err, remote)
			}
		})
	}
}

// The metadata reply decoder accepts only the trusted TYPE status words and
// canonical TTL integers delivered as text — unknown module types, malformed
// frames and redirects fail closed without touching payload allocation.
func TestRedisWorksheetMetadataReply(t *testing.T) {
	for _, tc := range []struct {
		name, command, line string
		value               any
		err                 error
		completed           bool
	}{
		{name: "type hash", command: "TYPE", line: "+hash", value: "hash", completed: true},
		{name: "type string", command: "TYPE", line: "+string", value: "string", completed: true},
		{name: "type none", command: "TYPE", line: "+none", value: "none", completed: true},
		{name: "ttl no expiry", command: "TTL", line: ":-1", value: "-1", completed: true},
		{name: "ttl missing", command: "TTL", line: ":-2", value: "-2", completed: true},
		{name: "ttl zero", command: "TTL", line: ":0", value: "0", completed: true},
		{name: "ttl precision-unsafe stays text", command: "TTL", line: ":9007199254740993", value: "9007199254740993", completed: true},
		{name: "ttl below floor", command: "TTL", line: ":-3", err: ErrQueryBackendFailure},
		{name: "ttl non-canonical sign", command: "TTL", line: ":+1", err: ErrQueryBackendFailure},
		{name: "ttl non-canonical zeros", command: "TTL", line: ":007", err: ErrQueryBackendFailure},
		{name: "ttl wrong marker", command: "TTL", line: "+1", err: ErrQueryBackendFailure},
		{name: "ttl status reply", command: "TTL", line: "+OK", err: ErrQueryBackendFailure},
		{name: "type untrusted module name", command: "TYPE", line: "+untrusted_module", err: ErrQueryBackendFailure},
		{name: "type integer reply", command: "TYPE", line: ":1", err: ErrQueryBackendFailure},
		{name: "type redirect", command: "TYPE", line: "-MOVED 1 :6379", err: ErrQueryBackendFailure},
		{name: "noperm denied", command: "TYPE", line: "-NOPERM denied", err: ErrQueryNotAllowed, completed: true},
		{name: "noperm denied ttl", command: "TTL", line: "-NOPERM denied", err: ErrQueryNotAllowed, completed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := model.QueryExecutionRemoteUnknown
			got, err := redisMetadataReply(tc.command, tc.line, &remote)
			if got != tc.value || !errors.Is(err, tc.err) || (remote == model.QueryExecutionRemoteCompleted) != tc.completed {
				t.Fatalf("value=%v error=%v remote=%s", got, err, remote)
			}
		})
	}
}

// The wire transaction is one static command after AUTH — anything before or
// after is a protocol break.
func TestRedisWorksheetMetadataWire(t *testing.T) {
	base := map[string]string{
		"AUTH": "+OK\r\n",
		"TYPE": "+hash\r\n",
		"TTL":  ":-1\r\n",
	}
	for _, tc := range []struct {
		name, command string
		replies       map[string]string
		value         any
		err           error
		completed     bool
		wantCommands  []string
	}{
		{name: "type value", command: "TYPE", value: "hash", completed: true, wantCommands: []string{"AUTH", "TYPE"}},
		{name: "ttl value", command: "TTL", value: "-1", completed: true, wantCommands: []string{"AUTH", "TTL"}},
		{name: "unknown command refused", command: "PTTL", err: ErrQueryValidationFailed, wantCommands: nil},
		{name: "noperm", command: "TYPE", replies: map[string]string{"TYPE": "-NOPERM denied\r\n"}, err: ErrQueryNotAllowed, completed: true, wantCommands: []string{"AUTH", "TYPE"}},
		{name: "redirect fails closed", command: "TYPE", replies: map[string]string{"TYPE": "-MOVED 1 :6379\r\n"}, err: ErrQueryBackendFailure, wantCommands: []string{"AUTH", "TYPE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replies := make(map[string]string, len(base)+len(tc.replies))
			for k, v := range base {
				replies[k] = v
			}
			for k, v := range tc.replies {
				replies[k] = v
			}
			server := startFakeRedisServer(t, replies)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			remote := model.QueryExecutionRemoteNone
			got, err := readRedisMetadata(ctx, server.listener.Addr().String(), "reader", "secret",
				server.clientTLSConfig(t), tc.command, "demo:order:101", &remote)
			if got != tc.value || !errors.Is(err, tc.err) || (remote == model.QueryExecutionRemoteCompleted) != tc.completed {
				t.Fatalf("value=%v error=%v remote=%s", got, err, remote)
			}
			if tc.wantCommands == nil {
				// A refused command never opens the connection.
				return
			}
			select {
			case <-server.done:
			case <-time.After(5 * time.Second):
				t.Fatal("fake server did not observe connection close")
			}
			if names := commandNames(server.received()); !slices.Equal(names, tc.wantCommands) {
				t.Fatalf("server saw commands %v, want %v", names, tc.wantCommands)
			}
		})
	}
}

// fakeRedisServer plays scripted RESP2 replies over one TLS connection and
// records every command it receives, so tests can prove the client stops
// before sending MULTI/HGET when the size probe refuses.
type fakeRedisServer struct {
	mu       sync.Mutex
	commands [][]string
	replies  map[string]string
	cert     tls.Certificate
	certPEM  []byte
	listener net.Listener
	done     chan struct{}
}

func startFakeRedisServer(t *testing.T, replies map[string]string) *fakeRedisServer {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeRedisServer{replies: replies, cert: cert, certPEM: certPEM, listener: listener, done: make(chan struct{})}
	go s.serve()
	t.Cleanup(func() { s.listener.Close() })
	return s
}

func (s *fakeRedisServer) clientTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(s.certPEM) {
		t.Fatal("fake server certificate could not be trusted")
	}
	return &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
}

func (s *fakeRedisServer) serve() {
	defer close(s.done)
	raw, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer raw.Close()
	conn := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		args, err := readFakeRedisCommand(reader)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.commands = append(s.commands, args)
		s.mu.Unlock()
		reply, ok := s.replies[strings.ToUpper(args[0])]
		if !ok {
			// An unexpected command means the client broke the fixed protocol.
			return
		}
		if _, err := conn.Write([]byte(reply)); err != nil {
			return
		}
	}
}

func readFakeRedisCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
	if err != nil || !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("bad command frame %q", line)
	}
	args := make([]string, count)
	for i := range args {
		head, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(head, "$")))
		if err != nil || !strings.HasPrefix(head, "$") {
			return nil, fmt.Errorf("bad command bulk %q", head)
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args[i] = string(buf[:length])
	}
	return args, nil
}

func (s *fakeRedisServer) received() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.commands)
}

func commandNames(commands [][]string) []string {
	names := make([]string, len(commands))
	for i, args := range commands {
		names[i] = strings.ToUpper(args[0])
	}
	return names
}

func TestRedisWorksheetHGETWire(t *testing.T) {
	base := map[string]string{
		"AUTH":    "+OK\r\n",
		"WATCH":   "+OK\r\n",
		"HSTRLEN": ":5\r\n",
		"MULTI":   "+OK\r\n",
		"HGET":    "+QUEUED\r\n",
		"EXEC":    "*1\r\n$5\r\nhello\r\n",
	}
	for _, tc := range []struct {
		name         string
		replies      map[string]string
		value        any
		err          error
		completed    bool
		wantCommands []string
	}{
		{name: "value", wantCommands: []string{"AUTH", "WATCH", "HSTRLEN", "MULTI", "HGET", "EXEC"}, value: "hello", completed: true},
		{name: "missing field", replies: map[string]string{"EXEC": "*1\r\n$-1\r\n"}, completed: true,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN", "MULTI", "HGET", "EXEC"}},
		{name: "empty field", replies: map[string]string{"EXEC": "*1\r\n$0\r\n\r\n"}, value: "", completed: true,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN", "MULTI", "HGET", "EXEC"}},
		{name: "over-limit stops before MULTI", replies: map[string]string{"HSTRLEN": ":1025\r\n"}, err: ErrQueryResultTooLarge, completed: true,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN"}},
		{name: "wrongtype stops before MULTI", replies: map[string]string{"HSTRLEN": "-WRONGTYPE not a hash\r\n"}, err: ErrQueryValidationFailed, completed: true,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN"}},
		{name: "noperm stops before MULTI", replies: map[string]string{"HSTRLEN": "-NOPERM denied\r\n"}, err: ErrQueryNotAllowed, completed: true,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN"}},
		{name: "watch noperm", replies: map[string]string{"WATCH": "-NOPERM denied\r\n"}, err: ErrQueryNotAllowed,
			wantCommands: []string{"AUTH", "WATCH"}},
		{name: "non-canonical hstrlen", replies: map[string]string{"HSTRLEN": ":007\r\n"}, err: ErrQueryBackendFailure,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN"}},
		{name: "concurrent write aborts EXEC", replies: map[string]string{"EXEC": "*-1\r\n"}, err: ErrQueryBackendFailure, completed: true,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN", "MULTI", "HGET", "EXEC"}},
		{name: "over-limit EXEC bulk never read", replies: map[string]string{"EXEC": "*1\r\n$1025\r\n"}, err: ErrQueryBackendFailure,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN", "MULTI", "HGET", "EXEC"}},
		{name: "wrongtype inside EXEC", replies: map[string]string{"EXEC": "*1\r\n-WRONGTYPE not a hash\r\n"}, err: ErrQueryValidationFailed, completed: true,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN", "MULTI", "HGET", "EXEC"}},
		{name: "invalid UTF-8", replies: map[string]string{"EXEC": "*1\r\n$1\r\n\xff\r\n"}, err: ErrQueryValidationFailed, completed: true,
			wantCommands: []string{"AUTH", "WATCH", "HSTRLEN", "MULTI", "HGET", "EXEC"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replies := make(map[string]string, len(base)+len(tc.replies))
			for k, v := range base {
				replies[k] = v
			}
			for k, v := range tc.replies {
				replies[k] = v
			}
			server := startFakeRedisServer(t, replies)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			remote := model.QueryExecutionRemoteNone
			got, err := readRedisHGET(ctx, server.listener.Addr().String(), "reader", "secret",
				server.clientTLSConfig(t), "demo:order:101", "status", &remote)
			if got != tc.value || !errors.Is(err, tc.err) || (remote == model.QueryExecutionRemoteCompleted) != tc.completed {
				t.Fatalf("value=%v error=%v remote=%s", got, err, remote)
			}
			select {
			case <-server.done:
			case <-time.After(5 * time.Second):
				t.Fatal("fake server did not observe connection close")
			}
			if names := commandNames(server.received()); !slices.Equal(names, tc.wantCommands) {
				t.Fatalf("server saw commands %v, want %v", names, tc.wantCommands)
			}
		})
	}
}
