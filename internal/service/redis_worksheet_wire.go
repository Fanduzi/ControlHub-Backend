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

// This connection speaks only the RESP2 frames for the fixed GET transaction.
// Bounds are checked before allocating payloads, including on malformed replies.
func readRedisGET(ctx context.Context, address, user, password string, config *tls.Config, key string, remote *model.QueryExecutionRemoteState) (any, error) {
	var dialer net.Dialer
	raw, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		return nil, err
	}
	conn := tls.Client(raw, config)
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	wire := redisWorksheetWire{Conn: conn, reader: bufio.NewReaderSize(conn, 2048)}
	if err := wire.expect("+OK", "AUTH", user, password); err != nil {
		return nil, err
	}
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

func (w redisWorksheetWire) expect(expected string, args ...string) error {
	var message bytes.Buffer
	fmt.Fprintf(&message, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&message, "$%d\r\n%s\r\n", len(arg), arg)
	}
	if _, err := w.Write(message.Bytes()); err != nil {
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
