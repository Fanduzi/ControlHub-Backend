//go:build integration

// Package integration validates the test-only result-reply observation barrier.
// input: mock MySQL packet peer and the plaintext fixture wire observer
// output: TestMySQLKeyedWireObserverReply
// pos: Self-test of command-forward versus first-server-reply facts; not a database or HTTP cancellation proof
// note: if this file changes, update this header and module README.md.
package integration

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestMySQLKeyedWireObserverReply(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	finish := make(chan struct{})
	t.Cleanup(func() { close(finish) })
	backendErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			backendErr <- err
			return
		}
		defer conn.Close()
		if _, err := readMySQLKeyedWireFrame(conn); err != nil {
			backendErr <- err
			return
		}
		if err := writeMySQLKeyedWireFrame(conn, makeMySQLKeyedWireFrame([]byte{1}, 1)); err != nil {
			backendErr <- err
			return
		}
		backendErr <- nil
		<-finish
	}()
	p := newMySQLKeyedWireObserver(t, listener.Addr().String(), "wire_fixture", "wire_result")
	arrived, release := p.HoldNextResultReply(t)
	defer release()
	client, err := net.Dial("tcp", p.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte{0x03}, "SELECT id FROM wire_fixture.wire_result"...)
	if err := writeMySQLKeyedWireFrame(client, makeMySQLKeyedWireFrame(payload, 0)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-arrived:
	case err := <-backendErr:
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("first server reply did not reach its observation barrier")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("selected result command was not forwarded")
	}
	deadline := time.Now().Add(5 * time.Second)
	for p.Counts().Results() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := p.Counts(); got != (mysqlKeyedWireCounts{Queries: 1, Replies: 1}) {
		t.Fatalf("reply barrier counts = %+v, want one forwarded query and one first reply", got)
	}
	if err := client.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = readMySQLKeyedWireFrame(client)
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("client read completed before the reply barrier was released: %v", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	release()
	frame, err := readMySQLKeyedWireFrame(client)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != 5 || frame[3] != 1 || frame[4] != 1 {
		t.Fatal("server reply was changed while crossing the observer")
	}
}
