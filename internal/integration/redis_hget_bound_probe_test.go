//go:build integration

package integration

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
	"sync/atomic"
	"testing"
	"time"
)

// TestRedisHGETBoundProbe is a feasibility probe for a governed `HGET key
// field` worksheet statement bounded to 1024 bytes at the server.
//
// Protocol under test (one RESP2 connection, after AUTH):
//  1. WATCH key            -> +OK
//  2. HSTRLEN key field    -> :n | -WRONGTYPE | -NOPERM
//  3. if n > 1024: stop, never send MULTI/HGET
//  4. else MULTI -> +OK, HGET key field -> +QUEUED, EXEC -> *1 bulk | *-1
//
// Claim: the client can never receive a field value > 1024 bytes, including
// when another client concurrently grows the field between step 2 and EXEC.
const probeValueLimit = 1024
const probeReaderMaxBulk = 2048

// countingConn counts plaintext bytes read from the socket at the net.Conn
// level (below bufio), so pipelined/buffered reply data is counted too.
type countingConn struct {
	net.Conn
	read atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

func (c *countingConn) bytesRead() int64 {
	return c.read.Load()
}

// respValue is a decoded RESP2 reply. line is the raw first line without the
// trailing CRLF (e.g. "+OK", ":5", "$1024", "*-1"); text is the payload for
// simple/error/bulk types; items holds array elements.
type respValue struct {
	kind    byte
	line    string
	text    string
	intVal  int64
	bulkLen int64
	items   []*respValue
}

func (v *respValue) describe() string {
	switch v.kind {
	case '$':
		if v.bulkLen == -1 {
			return "$-1"
		}
		payload := v.text
		if len(payload) > 40 {
			payload = payload[:40] + "..."
		}
		return fmt.Sprintf("$%d %q", v.bulkLen, payload)
	case '*':
		if v.bulkLen == -1 {
			return "*-1"
		}
		parts := make([]string, len(v.items))
		for i, item := range v.items {
			parts[i] = item.describe()
		}
		return fmt.Sprintf("*%d [%s]", v.bulkLen, strings.Join(parts, ", "))
	default:
		return v.line
	}
}

func readRESPLine(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		frag, err := r.ReadSlice('\n')
		line = append(line, frag...)
		if err == bufio.ErrBufferFull {
			if len(line) > 1<<16 {
				return "", fmt.Errorf("RESP line exceeds 64KiB")
			}
			continue
		}
		if err != nil {
			return "", err
		}
		break
	}
	if len(line) < 2 || !bytes.HasSuffix(line, []byte("\r\n")) {
		return "", fmt.Errorf("incomplete RESP frame %q", line)
	}
	return string(line[:len(line)-2]), nil
}

// readRESPValue reads one reply. A declared bulk length above maxBulk is
// recorded and reported without allocating or draining the payload — callers
// on the bounded reader connection treat that as a claim violation.
func readRESPValue(r *bufio.Reader, maxBulk int64) (*respValue, error) {
	line, err := readRESPLine(r)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 {
		return nil, fmt.Errorf("empty RESP frame")
	}
	v := &respValue{kind: line[0], line: line, bulkLen: -1}
	switch line[0] {
	case '+', '-':
		v.text = line[1:]
	case ':':
		v.intVal, err = strconv.ParseInt(line[1:], 10, 64)
		if err != nil {
			return v, fmt.Errorf("bad integer reply %q", line)
		}
		v.text = line[1:]
	case '$':
		n, err := strconv.ParseInt(line[1:], 10, 64)
		if err != nil || n < -1 {
			return v, fmt.Errorf("bad bulk header %q", line)
		}
		v.bulkLen = n
		if n == -1 {
			return v, nil
		}
		if n > maxBulk {
			return v, fmt.Errorf("bulk declares %d bytes > guard %d", n, maxBulk)
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return v, fmt.Errorf("read bulk payload: %w", err)
		}
		if !bytes.HasSuffix(buf, []byte("\r\n")) {
			return v, fmt.Errorf("bulk payload missing CRLF")
		}
		v.text = string(buf[:n])
	case '*':
		n, err := strconv.ParseInt(line[1:], 10, 64)
		if err != nil || n < -1 {
			return v, fmt.Errorf("bad array header %q", line)
		}
		v.bulkLen = n
		if n == -1 {
			return v, nil
		}
		if n > 1<<16 {
			return v, fmt.Errorf("array declares %d items", n)
		}
		for i := int64(0); i < n; i++ {
			item, err := readRESPValue(r, maxBulk)
			if err != nil {
				return v, err
			}
			v.items = append(v.items, item)
		}
	default:
		return v, fmt.Errorf("unknown RESP type %q", line)
	}
	return v, nil
}

func redisSend(conn net.Conn, args ...string) error {
	var request bytes.Buffer
	fmt.Fprintf(&request, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&request, "$%d\r\n%s\r\n", len(arg), arg)
	}
	_, err := conn.Write(request.Bytes())
	return err
}

// probeReader is a reader-user connection with byte counting and bounded
// reply parsing.
type probeReader struct {
	conn   *countingConn
	reader *bufio.Reader
}

func (p *probeReader) command(args ...string) (*respValue, error) {
	if err := p.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	if err := redisSend(p.conn, args...); err != nil {
		return nil, err
	}
	return readRESPValue(p.reader, probeReaderMaxBulk)
}

func (p *probeReader) close() {
	p.conn.Close()
}

// dialProbeRedis mirrors dialRedisFixture but counts bytes from the start of
// the connection, so the AUTH reply is included in the byte total.
func dialProbeRedis(ctx context.Context, address, user, password string, tlsConfig *tls.Config) (*probeReader, error) {
	dialer := tls.Dialer{Config: tlsConfig}
	raw, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	p := &probeReader{conn: &countingConn{Conn: raw}}
	p.reader = bufio.NewReaderSize(p.conn, 2048)
	reply, err := p.command("AUTH", user, password)
	if err != nil {
		raw.Close()
		return nil, err
	}
	if reply.kind != '+' {
		raw.Close()
		return nil, fmt.Errorf("AUTH %s: %s", user, reply.describe())
	}
	return p, nil
}

func adminDo(t *testing.T, admin redisFixtureConnection, args ...string) *respValue {
	t.Helper()
	if err := admin.SetDeadline(time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("admin deadline: %v", err)
	}
	if err := redisSend(admin.Conn, args...); err != nil {
		t.Fatalf("admin send %v: %v", args, err)
	}
	v, err := readRESPValue(admin.reader, 1<<26)
	if err != nil {
		t.Fatalf("admin read %v reply: %v", args, err)
	}
	return v
}

// adminExec fails only on an error ('-') reply; integer/simple replies are
// both accepted since setup commands mix HSET (:n) and SET/RENAME (+OK).
func adminExec(t *testing.T, admin redisFixtureConnection, args ...string) *respValue {
	t.Helper()
	v := adminDo(t, admin, args...)
	if v.kind == '-' {
		t.Fatalf("%v: %s", args, v.describe())
	}
	return v
}

// probeCmdstat returns the cumulative calls counter for a command from
// INFO commandstats, or 0 when the command never ran.
func probeCmdstat(t *testing.T, admin redisFixtureConnection, name string) int64 {
	t.Helper()
	v := adminDo(t, admin, "INFO", "commandstats")
	if v.kind != '$' {
		t.Fatalf("INFO commandstats: %s", v.describe())
	}
	prefix := "cmdstat_" + name + ":"
	for _, line := range strings.Split(v.text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		for _, field := range strings.Split(strings.TrimPrefix(line, prefix), ",") {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) == 2 && kv[0] == "calls" {
				n, err := strconv.ParseInt(kv[1], 10, 64)
				if err != nil {
					t.Fatalf("parse %s: %v", line, err)
				}
				return n
			}
		}
	}
	return 0
}

// hgetProbeOutcome records one run of the bounded-HGET protocol.
type hgetProbeOutcome struct {
	events     []string
	hstrlen    *respValue
	stopped    bool // protocol stopped at step 3 (HSTRLEN error or n > limit)
	execReply  *respValue
	delivered  int64 // bulk length delivered inside the EXEC array, -1 when none
	execBytes  int64 // reader socket bytes read between EXEC send and reply end
	totalBytes int64 // reader socket bytes read on the whole connection (incl. AUTH)
}

// runHGETBoundProbe executes the protocol on a fresh reader connection.
// mid, when non-nil, runs between the HSTRLEN reply and MULTI (still inside
// the WATCH..EXEC window). Any delivered bulk above probeValueLimit fails
// the test immediately: that is the claim being probed.
func runHGETBoundProbe(t *testing.T, ctx context.Context, address, readerPassword string, tlsConfig *tls.Config, key, field string, mid func()) *hgetProbeOutcome {
	t.Helper()
	p, err := dialProbeRedis(ctx, address, "reader", readerPassword, tlsConfig)
	if err != nil {
		t.Fatalf("dial reader: %v", err)
	}
	defer p.close()
	o := &hgetProbeOutcome{delivered: -1}
	roundtrip := func(args ...string) *respValue {
		o.events = append(o.events, ">> "+strings.Join(args, " "))
		v, err := p.command(args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		o.events = append(o.events, "<< "+v.describe())
		return v
	}

	if w := roundtrip("WATCH", key); w.kind != '+' {
		o.events = append(o.events, "WATCH not +OK; protocol cannot proceed")
		o.stopped = true
		o.totalBytes = p.conn.bytesRead()
		return o
	}
	hs := roundtrip("HSTRLEN", key, field)
	o.hstrlen = hs
	if hs.kind == '-' {
		o.stopped = true
		o.totalBytes = p.conn.bytesRead()
		return o
	}
	if hs.kind != ':' {
		t.Fatalf("HSTRLEN reply: %s", hs.describe())
	}
	if hs.intVal > probeValueLimit {
		o.stopped = true
		o.totalBytes = p.conn.bytesRead()
		return o
	}
	if mid != nil {
		mid()
	}
	m := roundtrip("MULTI")
	if m.kind != '+' {
		t.Fatalf("MULTI: %s", m.describe())
	}
	roundtrip("HGET", key, field)
	mark := p.conn.bytesRead()
	e := roundtrip("EXEC")
	o.execBytes = p.conn.bytesRead() - mark
	o.totalBytes = p.conn.bytesRead()
	o.execReply = e
	if e.kind == '*' && e.bulkLen > 0 && len(e.items) > 0 && e.items[0].kind == '$' {
		o.delivered = e.items[0].bulkLen
		if o.delivered > probeValueLimit {
			t.Fatalf("BOUND VIOLATION: EXEC delivered %d bytes > %d for %s %s", o.delivered, probeValueLimit, key, field)
		}
	}
	return o
}

func logOutcome(t *testing.T, o *hgetProbeOutcome) {
	t.Helper()
	for _, e := range o.events {
		t.Logf("  %s", e)
	}
}

func descReply(v *respValue) string {
	if v == nil {
		return "(none)"
	}
	return v.describe()
}

func TestRedisHGETBoundProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	address, adminPassword, readerPassword, _, tlsConfig := startRedisFixture(t)
	admin, err := dialRedisFixture(ctx, address, "fixture", adminPassword, tlsConfig)
	if err != nil {
		t.Fatalf("dial admin: %v", err)
	}
	defer admin.Close()

	if v := adminExec(t, admin, "ACL", "SETUSER", "reader",
		"-@all", "+multi", "+exists", "+getrange", "+exec", "+watch", "+hstrlen", "+hget", "resetkeys", "%R~demo:*"); v.kind != '+' {
		t.Fatalf("ACL SETUSER reader: %s", v.describe())
	}
	t.Logf("ACL GETUSER reader: %s", adminDo(t, admin, "ACL", "GETUSER", "reader").describe())
	t.Logf("CONFIG GET client-output-buffer-limit: %s", adminDo(t, admin, "CONFIG", "GET", "client-output-buffer-limit").describe())

	big10MiB := strings.Repeat("z", 10<<20)

	stats := func(t *testing.T) (int64, int64) {
		return probeCmdstat(t, admin, "hget"), probeCmdstat(t, admin, "hstrlen")
	}
	row := func(t *testing.T, format string, args ...any) {
		t.Logf("CASE "+format, args...)
	}

	t.Run("A_acl", func(t *testing.T) {
		h0, s0 := stats(t)
		adminDo(t, admin, "DEL", "demo:probe:h")
		adminExec(t, admin, "HSET", "demo:probe:h", "f", "v")
		p, err := dialProbeRedis(ctx, address, "reader", readerPassword, tlsConfig)
		if err != nil {
			t.Fatalf("dial reader: %v", err)
		}
		defer p.close()
		for _, args := range [][]string{
			{"WATCH", "demo:probe:h"},
			{"HSTRLEN", "demo:probe:h", "f"},
			{"HGET", "demo:probe:h", "f"},
			{"WATCH", "other:key"},
			{"HSTRLEN", "other:key", "f"},
			{"HSET", "demo:probe:h", "f", "x"},
			{"HGETALL", "demo:probe:h"},
			{"UNWATCH"},
			{"HSCAN", "demo:probe:h", "0"},
		} {
			v, err := p.command(args...)
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			t.Logf("A %s -> %s", strings.Join(args, " "), v.describe())
		}
		h1, s1 := stats(t)
		row(t, "A_acl bytes=%d hget_delta=%d hstrlen_delta=%d", p.conn.bytesRead(), h1-h0, s1-s0)
	})

	t.Run("B_normal", func(t *testing.T) {
		adminDo(t, admin, "DEL", "demo:probe:h")
		adminExec(t, admin, "HSET", "demo:probe:h", "status", "open", "region", "cn-east", "empty", "")
		adminDo(t, admin, "DEL", "demo:probe:absent")
		for _, tc := range []struct {
			name, key, field, want string
		}{
			{"status", "demo:probe:h", "status", "open"},
			{"region", "demo:probe:h", "region", "cn-east"},
			{"empty", "demo:probe:h", "empty", ""},
			{"missing_field", "demo:probe:h", "nope", ""},
			{"missing_key", "demo:probe:absent", "f", ""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h0, s0 := stats(t)
				o := runHGETBoundProbe(t, ctx, address, readerPassword, tlsConfig, tc.key, tc.field, nil)
				logOutcome(t, o)
				h1, s1 := stats(t)
				row(t, "B/%s exec=%s execBytes=%d totalBytes=%d hget_delta=%d hstrlen_delta=%d",
					tc.name, descReply(o.execReply), o.execBytes, o.totalBytes, h1-h0, s1-s0)
				if o.stopped {
					t.Fatalf("unexpected stop: hstrlen=%s", o.hstrlen.describe())
				}
				if tc.want != "" {
					if o.delivered != int64(len(tc.want)) || o.execReply.items[0].text != tc.want {
						t.Fatalf("want %q, got %s", tc.want, descReply(o.execReply))
					}
				}
			})
		}
	})

	t.Run("C_boundaries", func(t *testing.T) {
		adminExec(t, admin, "HSET", "demo:probe:c1024", "f", strings.Repeat("a", 1024))
		adminExec(t, admin, "HSET", "demo:probe:c1025", "f", strings.Repeat("b", 1025))
		adminExec(t, admin, "HSET", "demo:probe:cbig", "f", big10MiB)

		t.Run("exactly_1024", func(t *testing.T) {
			h0, s0 := stats(t)
			o := runHGETBoundProbe(t, ctx, address, readerPassword, tlsConfig, "demo:probe:c1024", "f", nil)
			logOutcome(t, o)
			h1, s1 := stats(t)
			row(t, "C/1024 exec=%s execBytes=%d totalBytes=%d hget_delta=%d hstrlen_delta=%d",
				descReply(o.execReply), o.execBytes, o.totalBytes, h1-h0, s1-s0)
			if o.delivered != 1024 {
				t.Fatalf("want delivered 1024, got %d (%s)", o.delivered, descReply(o.execReply))
			}
		})
		for _, tc := range []struct{ name, key string }{
			{"1025", "demo:probe:c1025"},
			{"10MiB", "demo:probe:cbig"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h0, s0 := stats(t)
				o := runHGETBoundProbe(t, ctx, address, readerPassword, tlsConfig, tc.key, "f", nil)
				logOutcome(t, o)
				h1, s1 := stats(t)
				row(t, "C/%s stopped=%v hstrlen=%s totalBytes=%d hget_delta=%d hstrlen_delta=%d",
					tc.name, o.stopped, o.hstrlen.describe(), o.totalBytes, h1-h0, s1-s0)
				if !o.stopped {
					t.Fatalf("expected stop at step 3, got EXEC %s", descReply(o.execReply))
				}
				if d := h1 - h0; d != 0 {
					t.Fatalf("BOUND VIOLATION: cmdstat_hget delta %d on stopped case", d)
				}
				if o.totalBytes >= 200 {
					t.Fatalf("BOUND VIOLATION: reader read %d bytes >= 200 on stopped case", o.totalBytes)
				}
			})
		}
	})

	t.Run("D_race_grow", func(t *testing.T) {
		adminExec(t, admin, "HSET", "demo:probe:race", "g", "small")
		h0, s0 := stats(t)
		o := runHGETBoundProbe(t, ctx, address, readerPassword, tlsConfig, "demo:probe:race", "g", func() {
			adminExec(t, admin, "HSET", "demo:probe:race", "g", big10MiB)
		})
		logOutcome(t, o)
		h1, s1 := stats(t)
		row(t, "D exec=%s execBytes=%d totalBytes=%d hget_delta=%d hstrlen_delta=%d",
			descReply(o.execReply), o.execBytes, o.totalBytes, h1-h0, s1-s0)
		if o.execReply == nil || o.execReply.kind != '*' || o.execReply.bulkLen != -1 {
			t.Fatalf("expected *-1 abort, got %s", descReply(o.execReply))
		}
		if o.execBytes >= 64 {
			t.Fatalf("BOUND VIOLATION: reader read %d bytes after EXEC >= 64", o.execBytes)
		}
		if d := h1 - h0; d != 0 {
			t.Fatalf("BOUND VIOLATION: cmdstat_hget delta %d on aborted EXEC", d)
		}
	})

	t.Run("E_race_modifications", func(t *testing.T) {
		type eCase struct {
			name  string
			setup func(t *testing.T)
			mid   func(t *testing.T)
		}
		cases := []eCase{
			{"different_field", func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e1", "g", "small", "other", "x")
			}, func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e1", "other", "y")
			}},
			{"del_key", func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e2", "g", "small")
			}, func(t *testing.T) {
				adminDo(t, admin, "DEL", "demo:probe:e2")
			}},
			{"set_type_change", func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e3", "g", "small")
			}, func(t *testing.T) {
				adminExec(t, admin, "SET", "demo:probe:e3", big10MiB)
			}},
			{"rename_over", func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e4", "g", "small")
				adminExec(t, admin, "HSET", "demo:probe:e4src", "g", big10MiB)
			}, func(t *testing.T) {
				adminExec(t, admin, "RENAME", "demo:probe:e4src", "demo:probe:e4")
			}},
			{"flushdb", func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e5", "g", "small")
			}, func(t *testing.T) {
				adminExec(t, admin, "FLUSHDB")
			}},
			{"key_expire", func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e6", "g", "small")
				t.Logf("PEXPIRE e6 100: %s", adminDo(t, admin, "PEXPIRE", "demo:probe:e6", "100").describe())
			}, func(t *testing.T) {
				time.Sleep(200 * time.Millisecond)
			}},
			{"hset_same_value", func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e7", "g", "small")
			}, func(t *testing.T) {
				adminExec(t, admin, "HSET", "demo:probe:e7", "g", "small")
			}},
		}
		for i, tc := range cases {
			key := fmt.Sprintf("demo:probe:e%d", i+1)
			t.Run(tc.name, func(t *testing.T) {
				tc.setup(t)
				h0, s0 := stats(t)
				o := runHGETBoundProbe(t, ctx, address, readerPassword, tlsConfig, key, "g", func() { tc.mid(t) })
				logOutcome(t, o)
				h1, s1 := stats(t)
				aborted := o.execReply != nil && o.execReply.kind == '*' && o.execReply.bulkLen == -1
				row(t, "E/%s exec=%s execBytes=%d totalBytes=%d hget_delta=%d hstrlen_delta=%d",
					tc.name, descReply(o.execReply), o.execBytes, o.totalBytes, h1-h0, s1-s0)
				if aborted && h1-h0 != 0 {
					t.Fatalf("BOUND VIOLATION: aborted EXEC but cmdstat_hget delta %d", h1-h0)
				}
			})
		}
	})

	t.Run("F_hash_field_ttl", func(t *testing.T) {
		t.Run("field_expire", func(t *testing.T) {
			adminExec(t, admin, "HSET", "demo:probe:f1", "g", "small")
			t.Logf("HPEXPIRE f1: %s", adminDo(t, admin, "HPEXPIRE", "demo:probe:f1", "100", "FIELDS", "1", "g").describe())
			h0, s0 := stats(t)
			o := runHGETBoundProbe(t, ctx, address, readerPassword, tlsConfig, "demo:probe:f1", "g", func() {
				time.Sleep(200 * time.Millisecond)
			})
			logOutcome(t, o)
			h1, s1 := stats(t)
			row(t, "F/field_expire exec=%s execBytes=%d totalBytes=%d hget_delta=%d hstrlen_delta=%d",
				descReply(o.execReply), o.execBytes, o.totalBytes, h1-h0, s1-s0)
		})
		t.Run("expire_then_regrow", func(t *testing.T) {
			adminExec(t, admin, "HSET", "demo:probe:f2", "g", "small")
			t.Logf("HPEXPIRE f2: %s", adminDo(t, admin, "HPEXPIRE", "demo:probe:f2", "100", "FIELDS", "1", "g").describe())
			h0, s0 := stats(t)
			o := runHGETBoundProbe(t, ctx, address, readerPassword, tlsConfig, "demo:probe:f2", "g", func() {
				time.Sleep(200 * time.Millisecond)
				adminExec(t, admin, "HSET", "demo:probe:f2", "g", big10MiB)
			})
			logOutcome(t, o)
			h1, s1 := stats(t)
			aborted := o.execReply != nil && o.execReply.kind == '*' && o.execReply.bulkLen == -1
			row(t, "F/expire_then_regrow exec=%s execBytes=%d totalBytes=%d hget_delta=%d hstrlen_delta=%d",
				descReply(o.execReply), o.execBytes, o.totalBytes, h1-h0, s1-s0)
			if !aborted {
				t.Fatalf("expected abort after field regrow, got %s", descReply(o.execReply))
			}
			if h1-h0 != 0 {
				t.Fatalf("BOUND VIOLATION: aborted EXEC but cmdstat_hget delta %d", h1-h0)
			}
		})
	})

	t.Run("G_non_hash", func(t *testing.T) {
		adminExec(t, admin, "SET", "demo:probe:s", "plain-string-value")
		t.Run("wrongtype", func(t *testing.T) {
			h0, s0 := stats(t)
			o := runHGETBoundProbe(t, ctx, address, readerPassword, tlsConfig, "demo:probe:s", "f", nil)
			logOutcome(t, o)
			h1, s1 := stats(t)
			row(t, "G/wrongtype hstrlen=%s stopped=%v totalBytes=%d hget_delta=%d hstrlen_delta=%d",
				o.hstrlen.describe(), o.stopped, o.totalBytes, h1-h0, s1-s0)
			if !o.stopped || o.hstrlen.kind != '-' {
				t.Fatalf("expected HSTRLEN error stop, got %s", o.hstrlen.describe())
			}
		})
		t.Run("get_path_regression", func(t *testing.T) {
			p, err := dialProbeRedis(ctx, address, "reader", readerPassword, tlsConfig)
			if err != nil {
				t.Fatalf("dial reader: %v", err)
			}
			defer p.close()
			for _, args := range [][]string{
				{"MULTI"},
				{"EXISTS", "demo:probe:s"},
				{"GETRANGE", "demo:probe:s", "0", "1024"},
				{"EXEC"},
			} {
				v, err := p.command(args...)
				if err != nil {
					t.Fatalf("%v: %v", args, err)
				}
				t.Logf("G %s -> %s", strings.Join(args, " "), v.describe())
				if args[0] == "EXEC" && (v.kind != '*' || v.bulkLen != 2) {
					t.Fatalf("GET-path EXEC: %s", v.describe())
				}
			}
			row(t, "G/get_path_regression bytes=%d", p.conn.bytesRead())
		})
	})

	t.Run("H_queue_acl", func(t *testing.T) {
		p, err := dialProbeRedis(ctx, address, "reader", readerPassword, tlsConfig)
		if err != nil {
			t.Fatalf("dial reader: %v", err)
		}
		defer p.close()
		for _, args := range [][]string{
			{"MULTI"},
			{"HGET", "other:key", "f"},
			{"EXEC"},
		} {
			v, err := p.command(args...)
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			t.Logf("H %s -> %s", strings.Join(args, " "), v.describe())
		}
		row(t, "H_queue_acl bytes=%d", p.conn.bytesRead())
	})
}
