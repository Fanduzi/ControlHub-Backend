//go:build integration

// Package integration observes MySQL-protocol result commands on isolated test targets.
// input: plaintext uncompressed fixture connections, Vitess table references, command frames
// output: mysqlKeyedWireObserver, separate command-forward/reply counts and request/reply test barriers
// pos: Test-only wire observation of selected result statements and first server replies, not server completion or execution evidence persistence
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"vitess.io/vitess/go/vt/sqlparser"
)

type mysqlKeyedWireCounts struct {
	Queries     uint64
	Prepares    uint64
	Executes    uint64
	Replies     uint64
	ReplyErrors uint64
}

func (c mysqlKeyedWireCounts) Results() uint64 { return c.Queries + c.Executes }

// mysqlKeyedWireMetaCounts separately tracks commands addressed to the
// observer's optional metadata selector (e.g. information_schema.COLUMNS) —
// preflight and inspector traffic is real wire work, but it is never a
// result command and must stay out of Results.
type mysqlKeyedWireMetaCounts struct {
	Queries  uint64
	Prepares uint64
	Executes uint64
}

type mysqlKeyedWireGate struct {
	arrived chan struct{}
	release chan struct{}
	once    sync.Once
}

type mysqlKeyedWireObserver struct {
	listener     net.Listener
	backend      string
	database     string
	table        string
	metaDatabase string
	metaTable    string
	parser       *sqlparser.Parser
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	counts       mysqlKeyedWireCounts
	meta         mysqlKeyedWireMetaCounts
	conns        map[net.Conn]struct{}
	gate         *mysqlKeyedWireGate
	replyGate    *mysqlKeyedWireGate
}

type mysqlKeyedWireState struct {
	mu                  sync.Mutex
	pendingPrepare      bool
	pendingSelected     bool
	pendingSelectedMeta bool
	pendingResult       bool
	selected            map[uint32]bool
	selectedMeta        map[uint32]bool
}

func newMySQLKeyedWireObserver(t *testing.T, backend, database, table string) *mysqlKeyedWireObserver {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &mysqlKeyedWireObserver{
		listener: listener, backend: backend, database: database, table: table,
		parser: parser, ctx: ctx, cancel: cancel, conns: make(map[net.Conn]struct{}),
	}
	p.wg.Add(1)
	go p.accept()
	t.Cleanup(func() {
		p.cancel()
		p.listener.Close()
		p.mu.Lock()
		for conn := range p.conns {
			conn.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
	return p
}

func (p *mysqlKeyedWireObserver) Address() string { return p.listener.Addr().String() }

func (p *mysqlKeyedWireObserver) Counts() mysqlKeyedWireCounts {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts
}

// WithMetadataTable arms the optional second selector
// (<database>.<table>) so schema-metadata commands — information_schema
// preflight/inspector traffic — are counted separately in MetaCounts and
// never leak into the result counters. Returns p for chaining.
func (p *mysqlKeyedWireObserver) WithMetadataTable(database, table string) *mysqlKeyedWireObserver {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.metaDatabase, p.metaTable = database, table
	return p
}

func (p *mysqlKeyedWireObserver) MetaCounts() mysqlKeyedWireMetaCounts {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.meta
}

func (p *mysqlKeyedWireObserver) HoldNextResult(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gate != nil {
		t.Fatal("result wire barrier is already installed")
	}
	gate := &mysqlKeyedWireGate{arrived: make(chan struct{}), release: make(chan struct{})}
	p.gate = gate
	return gate.arrived, func() { gate.once.Do(func() { close(gate.release) }) }
}

func (p *mysqlKeyedWireObserver) HoldNextResultReply(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.replyGate != nil {
		t.Fatal("result reply barrier is already installed")
	}
	gate := &mysqlKeyedWireGate{arrived: make(chan struct{}), release: make(chan struct{})}
	p.replyGate = gate
	return gate.arrived, func() { gate.once.Do(func() { close(gate.release) }) }
}

func (p *mysqlKeyedWireObserver) beforeResult() bool {
	p.mu.Lock()
	gate := p.gate
	p.gate = nil
	p.mu.Unlock()
	return p.awaitGate(gate)
}

func (p *mysqlKeyedWireObserver) beforeResultReply() bool {
	p.mu.Lock()
	gate := p.replyGate
	p.replyGate = nil
	p.mu.Unlock()
	return p.awaitGate(gate)
}

func (p *mysqlKeyedWireObserver) awaitGate(gate *mysqlKeyedWireGate) bool {
	if gate == nil {
		return true
	}
	close(gate.arrived)
	select {
	case <-gate.release:
		return true
	case <-p.ctx.Done():
		return false
	}
}

func (p *mysqlKeyedWireObserver) selectedSQL(query string) bool {
	p.mu.Lock()
	database, table := p.database, p.table
	p.mu.Unlock()
	return p.selectedSQLFor(query, database, table)
}

// selectedSQLFor reports whether the parsed statement names the given
// qualified table anywhere in its AST — the same real-parser walk used for
// the result selector, reused for the metadata selector.
func (p *mysqlKeyedWireObserver) selectedSQLFor(query, database, table string) bool {
	if database == "" || table == "" {
		return false
	}
	stmt, err := p.parser.Parse(query)
	if err != nil {
		return false
	}
	if _, ok := stmt.(*sqlparser.Select); !ok {
		return false
	}
	selected := false
	_ = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		if tn, ok := node.(sqlparser.TableName); ok && tn.Qualifier.String() == database && tn.Name.String() == table {
			selected = true
		}
		return true, nil
	}, stmt)
	return selected
}

func (p *mysqlKeyedWireObserver) accept() {
	defer p.wg.Done()
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		if p.ctx.Err() != nil {
			p.mu.Unlock()
			client.Close()
			return
		}
		p.conns[client] = struct{}{}
		p.wg.Add(1)
		p.mu.Unlock()
		go p.forward(client)
	}
}

func (p *mysqlKeyedWireObserver) forward(client net.Conn) {
	defer p.wg.Done()
	defer client.Close()
	defer func() {
		p.mu.Lock()
		delete(p.conns, client)
		p.mu.Unlock()
	}()
	backend, err := (&net.Dialer{}).DialContext(p.ctx, "tcp", p.backend)
	if err != nil {
		return
	}
	defer backend.Close()
	p.mu.Lock()
	if p.ctx.Err() != nil {
		p.mu.Unlock()
		return
	}
	p.conns[backend] = struct{}{}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.conns, backend)
		p.mu.Unlock()
	}()
	state := &mysqlKeyedWireState{selected: make(map[uint32]bool), selectedMeta: make(map[uint32]bool)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer client.Close()
		defer backend.Close()
		for {
			frame, err := readMySQLKeyedWireFrame(backend)
			if err != nil {
				return
			}
			payload := frame[4:]
			resultReply := false
			state.mu.Lock()
			if frame[3] == 1 && state.pendingPrepare {
				if len(payload) >= 12 && payload[0] == 0x00 {
					stmtID := binary.LittleEndian.Uint32(payload[1:5])
					state.selected[stmtID] = state.pendingSelected
					state.selectedMeta[stmtID] = state.pendingSelectedMeta
				}
				state.pendingPrepare = false
			}
			if frame[3] == 1 && state.pendingResult {
				resultReply = true
				state.pendingResult = false
			}
			state.mu.Unlock()
			if resultReply {
				p.mu.Lock()
				p.counts.Replies++
				if len(payload) > 0 && payload[0] == 0xff {
					p.counts.ReplyErrors++
				}
				p.mu.Unlock()
				if !p.beforeResultReply() {
					return
				}
			}
			if err := writeMySQLKeyedWireFrame(client, frame); err != nil {
				return
			}
		}
	}()
	defer func() {
		client.Close()
		backend.Close()
		<-done
	}()
	for {
		frame, err := readMySQLKeyedWireFrame(client)
		if err != nil {
			return
		}
		payload := frame[4:]
		var query, prepare, execute, metaQuery, metaPrepare, metaExecute bool
		if frame[3] == 0 && len(payload) > 0 {
			switch payload[0] {
			case 0x03:
				queryText := string(payload[1:])
				p.mu.Lock()
				resDB, resTable := p.database, p.table
				metaDB, metaTable := p.metaDatabase, p.metaTable
				p.mu.Unlock()
				query = p.selectedSQLFor(queryText, resDB, resTable)
				metaQuery = p.selectedSQLFor(queryText, metaDB, metaTable)
			case 0x16:
				queryText := string(payload[1:])
				p.mu.Lock()
				resDB, resTable := p.database, p.table
				metaDB, metaTable := p.metaDatabase, p.metaTable
				p.mu.Unlock()
				prepare = p.selectedSQLFor(queryText, resDB, resTable)
				metaPrepare = p.selectedSQLFor(queryText, metaDB, metaTable)
				state.mu.Lock()
				state.pendingPrepare, state.pendingSelected, state.pendingSelectedMeta = true, prepare, metaPrepare
				state.mu.Unlock()
			case 0x17:
				if len(payload) >= 10 {
					state.mu.Lock()
					stmtID := binary.LittleEndian.Uint32(payload[1:5])
					execute = state.selected[stmtID]
					metaExecute = state.selectedMeta[stmtID]
					state.mu.Unlock()
				}
			case 0x19:
				if len(payload) >= 5 {
					state.mu.Lock()
					stmtID := binary.LittleEndian.Uint32(payload[1:5])
					delete(state.selected, stmtID)
					delete(state.selectedMeta, stmtID)
					state.mu.Unlock()
				}
			}
		}
		if query || execute {
			state.mu.Lock()
			state.pendingResult = true
			state.mu.Unlock()
			if !p.beforeResult() {
				return
			}
		}
		if err := writeMySQLKeyedWireFrame(backend, frame); err != nil {
			return
		}
		p.mu.Lock()
		if query {
			p.counts.Queries++
		}
		if prepare {
			p.counts.Prepares++
		}
		if execute {
			p.counts.Executes++
		}
		if metaQuery {
			p.meta.Queries++
		}
		if metaPrepare {
			p.meta.Prepares++
		}
		if metaExecute {
			p.meta.Executes++
		}
		p.mu.Unlock()
	}
}

func readMySQLKeyedWireFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	frame := make([]byte, 4+size)
	copy(frame, header[:])
	_, err := io.ReadFull(r, frame[4:])
	return frame, err
}

func writeMySQLKeyedWireFrame(w io.Writer, frame []byte) error {
	for len(frame) > 0 {
		n, err := w.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func makeMySQLKeyedWireFrame(payload []byte, sequence byte) []byte {
	n := len(payload)
	return append([]byte{byte(n), byte(n >> 8), byte(n >> 16), sequence}, payload...)
}

func TestMySQLKeyedWireObserver(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		for i, id := 0, uint32(7); i < 9; i++ {
			frame, err := readMySQLKeyedWireFrame(conn)
			if err != nil {
				done <- err
				return
			}
			if frame[4] == 0x16 {
				payload := make([]byte, 12)
				binary.LittleEndian.PutUint32(payload[1:5], id)
				id++
				if err := writeMySQLKeyedWireFrame(conn, makeMySQLKeyedWireFrame(payload, 1)); err != nil {
					done <- err
					return
				}
			}
		}
		done <- nil
	}()
	p := newMySQLKeyedWireObserver(t, listener.Addr().String(), "wire_fixture", "wire_result")
	client, err := net.Dial("tcp", p.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	statement := func(command byte, id uint32) []byte {
		payload := make([]byte, 5)
		payload[0] = command
		binary.LittleEndian.PutUint32(payload[1:], id)
		if command == 0x17 {
			payload = append(payload, 0, 1, 0, 0, 0)
		}
		return payload
	}
	messages := [][]byte{
		append([]byte{0x16}, "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_NAME = 'wire_result'"...),
		statement(0x17, 7),
		statement(0x19, 7),
		append([]byte{0x16}, "SELECT id FROM wire_fixture.wire_result WHERE id = ?"...),
		statement(0x17, 999),
		statement(0x17, 8),
		append([]byte{0x03}, "SELECT id FROM wire_fixture.wire_result LIMIT 1"...),
		statement(0x19, 8),
		statement(0x17, 8),
	}
	arrived, release := p.HoldNextResult(t)
	defer release()
	for i, payload := range messages {
		if err := writeMySQLKeyedWireFrame(client, makeMySQLKeyedWireFrame(payload, 0)); err != nil {
			t.Fatal(err)
		}
		if i == 5 {
			select {
			case <-arrived:
			case <-time.After(5 * time.Second):
				t.Fatal("selected execute did not reach the wire barrier")
			}
			if got := p.Counts(); got.Results() != 0 || got.Prepares != 1 {
				t.Fatalf("barrier counts = %+v, want only one prepared result statement", got)
			}
			release()
		}
		if payload[0] == 0x16 {
			if _, err := readMySQLKeyedWireFrame(client); err != nil {
				t.Fatal(err)
			}
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wire observer did not forward all fixture frames")
	}
	if got := p.Counts(); got != (mysqlKeyedWireCounts{Queries: 1, Prepares: 1, Executes: 1}) {
		t.Fatalf("forwarded counts = %+v", got)
	}
	for _, query := range []string{
		"SELECT ' from wire_fixture.wire_result' AS literal_only",
		"SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_NAME = 'wire_result'",
		"SHOW COLUMNS FROM wire_fixture.wire_result",
		"SELECT id FROM other_fixture.wire_result",
	} {
		if p.selectedSQL(query) {
			t.Fatalf("non-result statement selected: %s", strings.Split(query, " ")[0])
		}
	}
	if !p.selectedSQL("SELECT r.id FROM `wire_fixture`.`wire_result` AS r") {
		t.Fatal("quoted result relation was not selected")
	}
}
