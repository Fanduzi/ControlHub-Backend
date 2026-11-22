package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/model"
)

const redisWorksheetValueLimit = 1024

type redisWorksheetWire struct {
	net.Conn
	reader *bufio.Reader
}

// dialRedisWorksheet opens the short-lived authenticated RESP2 connection
// shared by the fixed GET and HGET transactions. The returned cleanup stops
// the cancellation watcher and closes the socket, in that order.
func dialRedisWorksheet(ctx context.Context, address, user, password string, config *tls.Config) (redisWorksheetWire, func(), error) {
	var dialer net.Dialer
	raw, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return redisWorksheetWire{}, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	cleanup := func() {
		stop()
		_ = raw.Close()
	}
	deadline, _ := ctx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		cleanup()
		return redisWorksheetWire{}, nil, err
	}
	conn := tls.Client(raw, config)
	if err := conn.HandshakeContext(ctx); err != nil {
		cleanup()
		return redisWorksheetWire{}, nil, err
	}
	wire := redisWorksheetWire{Conn: conn, reader: bufio.NewReaderSize(conn, 2048)}
	if err := wire.expect("+OK", "AUTH", user, password); err != nil {
		cleanup()
		return redisWorksheetWire{}, nil, err
	}
	return wire, cleanup, nil
}

// This connection speaks only the RESP2 frames for the fixed GET transaction.
// Bounds are checked before allocating payloads, including on malformed replies.
func readRedisGET(ctx context.Context, address, user, password string, config *tls.Config, key string, remote *model.QueryExecutionRemoteState) (any, error) {
	wire, cleanup, err := dialRedisWorksheet(ctx, address, user, password, config)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := wire.expect("+OK", "MULTI"); err != nil {
		return nil, err
	}
	if err := wire.expect("+QUEUED", "EXISTS", key); err != nil {
		return nil, err
	}
	// The inclusive upper offset probes one byte past the allowed value.
	if err := wire.expect("+QUEUED", "GETRANGE", key, "0", strconv.Itoa(redisWorksheetValueLimit)); err != nil {
		return nil, err
	}
	*remote = model.QueryExecutionRemoteUnknown
	if err := wire.expect("*2", "EXEC"); err != nil {
		return nil, err
	}
	return wire.getReply(remote)
}

// This connection speaks only the RESP2 frames for the fixed HGET transaction.
// WATCH makes EXEC abort on any concurrent write after HSTRLEN, so an oversized
// field can never be transferred: the size probe runs outside MULTI and EXEC
// returns a nil array instead of executing the queued HGET.
func readRedisHGET(ctx context.Context, address, user, password string, config *tls.Config, key, field string, remote *model.QueryExecutionRemoteState) (any, error) {
	wire, cleanup, err := dialRedisWorksheet(ctx, address, user, password, config)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	// WATCH and HSTRLEN are real commands outside the transaction, so a
	// disconnect can no longer prove the server saw nothing.
	*remote = model.QueryExecutionRemoteUnknown
	if err := wire.expect("+OK", "WATCH", key); err != nil {
		return nil, err
	}
	if err := wire.send("HSTRLEN", key, field); err != nil {
		return nil, err
	}
	header, err := wire.line()
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(header, "-WRONGTYPE "):
		*remote = model.QueryExecutionRemoteCompleted
		return nil, ErrQueryValidationFailed
	case strings.HasPrefix(header, "-NOPERM "):
		*remote = model.QueryExecutionRemoteCompleted
		return nil, ErrQueryNotAllowed
	}
	size, ok := strings.CutPrefix(header, ":")
	length, err := strconv.Atoi(size)
	if !ok || err != nil || length < 0 || strconv.Itoa(length) != size {
		return nil, ErrQueryBackendFailure
	}
	if length > redisWorksheetValueLimit {
		*remote = model.QueryExecutionRemoteCompleted
		return nil, ErrQueryResultTooLarge
	}
	if err := wire.expect("+OK", "MULTI"); err != nil {
		return nil, err
	}
	if err := wire.expect("+QUEUED", "HGET", key, field); err != nil {
		return nil, err
	}
	if err := wire.send("EXEC"); err != nil {
		return nil, err
	}
	reply, err := wire.line()
	if err != nil {
		return nil, err
	}
	switch reply {
	case "*-1":
		// A watched key changed since HSTRLEN: EXEC aborted and HGET never ran.
		*remote = model.QueryExecutionRemoteCompleted
		return nil, ErrQueryBackendFailure
	case "*1":
		return wire.hgetReply(remote)
	default:
		return nil, ErrQueryBackendFailure
	}
}

func (w redisWorksheetWire) send(args ...string) error {
	var message bytes.Buffer
	fmt.Fprintf(&message, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&message, "$%d\r\n%s\r\n", len(arg), arg)
	}
	_, err := w.Write(message.Bytes())
	return err
}

func (w redisWorksheetWire) expect(expected string, args ...string) error {
	if err := w.send(args...); err != nil {
		return err
	}
	line, err := w.line()
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "-NOPERM ") {
		return ErrQueryNotAllowed
	}
	if line != expected {
		return ErrQueryBackendFailure
	}
	return nil
}

func (w redisWorksheetWire) line() (string, error) {
	line, err := w.reader.ReadSlice('\n')
	if err != nil {
		return "", err
	}
	if !bytes.HasSuffix(line, []byte("\r\n")) {
		return "", ErrQueryBackendFailure
	}
	return string(line[:len(line)-2]), nil
}

func (w redisWorksheetWire) getReply(remote *model.QueryExecutionRemoteState) (any, error) {
	exists, err := w.line()
	if err != nil {
		return nil, err
	}
	if exists != ":0" && exists != ":1" {
		if strings.HasPrefix(exists, "-NOPERM ") {
			return nil, ErrQueryNotAllowed
		}
		return nil, ErrQueryBackendFailure
	}
	header, err := w.line()
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(header, "-WRONGTYPE "):
		*remote = model.QueryExecutionRemoteCompleted
		return nil, ErrQueryValidationFailed
	case strings.HasPrefix(header, "-NOPERM "):
		*remote = model.QueryExecutionRemoteCompleted
		return nil, ErrQueryNotAllowed
	}
	size, ok := strings.CutPrefix(header, "$")
	length, err := strconv.Atoi(size)
	if !ok || err != nil || length < 0 || length > redisWorksheetValueLimit+1 || strconv.Itoa(length) != size {
		return nil, ErrQueryBackendFailure
	}
	payload := make([]byte, length+2)
	if _, err := io.ReadFull(w.reader, payload); err != nil {
		return nil, err
	}
	if !bytes.Equal(payload[length:], []byte("\r\n")) {
		return nil, ErrQueryBackendFailure
	}
	*remote = model.QueryExecutionRemoteCompleted
	if length > redisWorksheetValueLimit {
		return nil, ErrQueryResultTooLarge
	}
	if !utf8.Valid(payload[:length]) {
		return nil, ErrQueryValidationFailed
	}
	if exists == ":0" {
		if length > 0 {
			return nil, ErrQueryBackendFailure
		}
		return nil, nil
	}
	return string(payload[:length]), nil
}

// hgetReply reads the single EXEC array item of the fixed HGET transaction.
// The declared bulk length is checked against the bound before any payload
// byte is read or allocated.
func (w redisWorksheetWire) hgetReply(remote *model.QueryExecutionRemoteState) (any, error) {
	header, err := w.line()
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(header, "-WRONGTYPE "):
		*remote = model.QueryExecutionRemoteCompleted
		return nil, ErrQueryValidationFailed
	case strings.HasPrefix(header, "-NOPERM "):
		*remote = model.QueryExecutionRemoteCompleted
		return nil, ErrQueryNotAllowed
	case header == "$-1":
		*remote = model.QueryExecutionRemoteCompleted
		return nil, nil
	}
	size, ok := strings.CutPrefix(header, "$")
	length, err := strconv.Atoi(size)
	if !ok || err != nil || length < 0 || length > redisWorksheetValueLimit || strconv.Itoa(length) != size {
		return nil, ErrQueryBackendFailure
	}
	payload := make([]byte, length+2)
	if _, err := io.ReadFull(w.reader, payload); err != nil {
		return nil, err
	}
	if !bytes.Equal(payload[length:], []byte("\r\n")) {
		return nil, ErrQueryBackendFailure
	}
	*remote = model.QueryExecutionRemoteCompleted
	if !utf8.Valid(payload[:length]) {
		return nil, ErrQueryValidationFailed
	}
	return string(payload[:length]), nil
}
