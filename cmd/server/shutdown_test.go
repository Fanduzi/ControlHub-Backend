// Package main provides tests for the ControlHub application entry point.
// input: fmt, io, net, net/http, os, os/signal, sync, syscall, testing, time
// output: TestRunServer_* lifecycle tests, TestRunServer_TerminationSignalsBeginGracefulDrain
// pos: Deterministic lifecycle tests for the graceful drain seam — traffic stop, successful drain, bound exhaustion, second-signal force exit, server failure, the fixed-one-message log contract, and real SIGTERM/SIGINT wiring (Issue #37)
// note: if this file changes, update header and cmd/server/README.md
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"
)

// logCapture records the fixed shutdown log messages for assertion.
type logCapture struct {
	mu   sync.Mutex
	msgs []string
}

func newLogCapture() *logCapture {
	return &logCapture{}
}

func (c *logCapture) logf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, fmt.Sprintf(format, args...))
}

func (c *logCapture) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.msgs))
	copy(out, c.msgs)
	return out
}

// assertOneMessage asserts the shutdown emitted exactly one fixed log message
// with the exact expected text — the Issue #37 "only a fixed safe log" contract.
func assertOneMessage(t *testing.T, logs *logCapture, want string) {
	t.Helper()
	got := logs.messages()
	if len(got) != 1 {
		t.Fatalf("shutdown log count = %d (%q), want exactly one %q", len(got), got, want)
	}
	if got[0] != want {
		t.Fatalf("shutdown log = %q, want %q", got[0], want)
	}
}

// startTestServer binds a listener on an ephemeral port and returns the
// server and listener used by runServer.
func startTestServer(t *testing.T, handler http.Handler) (*http.Server, net.Listener) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return &http.Server{Handler: handler}, ln
}

// exitAfter runs runServer in a goroutine and returns its exit code channel.
func exitAfter(srv *http.Server, ln net.Listener, sigs chan os.Signal, drain time.Duration, logs *logCapture) chan int {
	exit := make(chan int, 1)
	go func() { exit <- runServer(srv, ln, sigs, drain, logs.logf) }()
	return exit
}

// waitForStoppedTraffic proves the listener stopped accepting new
// connections: after shutdown begins, dials must fail. The handler stays
// in-flight, so the drain cannot have completed; a refused dial is therefore
// traffic stop, not post-drain state.
func waitForStoppedTraffic(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("new connections still accepted after shutdown began")
}

// waitInFlight waits for the handler to be active, failing fast when the
// client request errors or the handler is never reached (regression guard).
func waitInFlight(t *testing.T, inFlight <-chan struct{}, clientErr <-chan error) {
	t.Helper()
	select {
	case <-inFlight:
	case err := <-clientErr:
		t.Fatalf("client request failed before reaching the handler: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("handler never reached in time")
	}
}

// TestRunServer_SignalStopsTrafficAndDrainsInFlightRequest proves one
// shutdown signal stops new traffic while a handler is still active, lets the
// in-flight handler finish during the drain window, exits 0, and emits the
// single fixed clean-drain log.
func TestRunServer_SignalStopsTrafficAndDrainsInFlightRequest(t *testing.T) {
	release := make(chan struct{})
	inFlight := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	})
	srv, ln := startTestServer(t, handler)
	sigs := make(chan os.Signal, 2)
	logs := newLogCapture()
	exit := exitAfter(srv, ln, sigs, 0, logs)

	addr := ln.Addr().String()
	got := make(chan *http.Response, 1)
	gotErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			gotErr <- err
			return
		}
		got <- resp
	}()
	waitInFlight(t, inFlight, gotErr)

	// Shutdown signal: new traffic must stop while the handler is still active.
	sigs <- syscall.SIGTERM
	waitForStoppedTraffic(t, addr)

	// The in-flight handler may finish within the drain window; clean exit 0.
	close(release)
	select {
	case code := <-exit:
		if code != 0 {
			t.Fatalf("clean drain exit code = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("clean drain did not complete in time")
	}
	assertOneMessage(t, logs, "ControlHub shutdown drain complete; exiting")

	select {
	case resp := <-got:
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read in-flight response: %v", err)
		}
		if string(body) != "done" {
			t.Fatalf("in-flight response body = %q, want %q", body, "done")
		}
	case err := <-gotErr:
		t.Fatalf("in-flight request failed during drain: %v", err)
	}
}

// TestRunServer_DrainDeadlineExhaustionExitsNonZero proves a handler that
// never finishes does not hold the process: new traffic stops, shutdown
// returns once the drain bound elapses, the process exits 1 without waiting
// indefinitely, and the single fixed deadline log is emitted.
func TestRunServer_DrainDeadlineExhaustionExitsNonZero(t *testing.T) {
	blocked := make(chan struct{})
	inFlight := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		<-blocked
	})
	srv, ln := startTestServer(t, handler)
	sigs := make(chan os.Signal, 2)
	logs := newLogCapture()
	const drain = 40 * time.Millisecond
	exit := exitAfter(srv, ln, sigs, drain, logs)

	addr := ln.Addr().String()
	clientErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			clientErr <- err
			return
		}
		_ = resp.Body.Close()
	}()
	waitInFlight(t, inFlight, clientErr)

	start := time.Now()
	sigs <- syscall.SIGTERM
	waitForStoppedTraffic(t, addr)

	select {
	case code := <-exit:
		if code != 1 {
			t.Fatalf("drain deadline exit code = %d, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drain deadline exhaustion did not return in time")
	}
	if elapsed := time.Since(start); elapsed < drain {
		t.Fatalf("shutdown returned before the drain bound elapsed: %s < %s", elapsed, drain)
	}
	assertOneMessage(t, logs, "ControlHub shutdown drain deadline exceeded; exiting")
	close(blocked)
}

// TestRunServer_SecondSignalForcesImmediateExit proves a second shutdown
// signal during the drain cuts the wait short instead of honoring the full
// bound: the process must exit 1 promptly, never waiting indefinitely.
func TestRunServer_SecondSignalForcesImmediateExit(t *testing.T) {
	blocked := make(chan struct{})
	inFlight := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		<-blocked
	})
	srv, ln := startTestServer(t, handler)
	sigs := make(chan os.Signal, 2)
	logs := newLogCapture()
	exit := exitAfter(srv, ln, sigs, 30*time.Second, logs)

	clientErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			clientErr <- err
			return
		}
		_ = resp.Body.Close()
	}()
	waitInFlight(t, inFlight, clientErr)

	sigs <- syscall.SIGINT
	sigs <- syscall.SIGINT // second signal must force immediate exit

	select {
	case code := <-exit:
		if code != 1 {
			t.Fatalf("second-signal exit code = %d, want 1", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second shutdown signal did not force immediate exit")
	}
	assertOneMessage(t, logs, "ControlHub second shutdown signal; exiting immediately")
	close(blocked)
}

// TestRunServer_ServerFailureExitsNonZero proves an HTTP server failure that
// is not a normal close exits 1 after exactly one fixed safe log. The
// listener is already closed, so Serve fails immediately.
func TestRunServer_ServerFailureExitsNonZero(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	sigs := make(chan os.Signal, 1)
	logs := newLogCapture()
	exit := exitAfter(srv, ln, sigs, 0, logs)

	select {
	case code := <-exit:
		if code != 1 {
			t.Fatalf("server failure exit code = %d, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server failure did not exit in time")
	}
	assertOneMessage(t, logs, "ControlHub http server failed; exiting")
}

// TestRunServer_TerminationSignalsBeginGracefulDrain proves main's wiring
// contract: a real SIGTERM and a real SIGINT delivered to the process both
// begin the graceful drain (traffic stop while a handler is in flight, clean
// exit 0, single fixed clean-drain log).
func TestRunServer_TerminationSignalsBeginGracefulDrain(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		sig := sig
		t.Run(sig.String(), func(t *testing.T) {
			release := make(chan struct{})
			inFlight := make(chan struct{})
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(inFlight)
				<-release
				w.WriteHeader(http.StatusOK)
			})
			srv, ln := startTestServer(t, handler)
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, sig)
			t.Cleanup(func() { signal.Stop(sigs) })
			logs := newLogCapture()
			exit := exitAfter(srv, ln, sigs, 0, logs)

			clientErr := make(chan error, 1)
			go func() {
				resp, err := http.Get("http://" + ln.Addr().String() + "/")
				if err != nil {
					clientErr <- err
					return
				}
				_ = resp.Body.Close()
			}()
			waitInFlight(t, inFlight, clientErr)

			if err := syscall.Kill(os.Getpid(), sig.(syscall.Signal)); err != nil {
				t.Fatalf("deliver %v: %v", sig, err)
			}
			waitForStoppedTraffic(t, ln.Addr().String())

			close(release)
			select {
			case code := <-exit:
				if code != 0 {
					t.Fatalf("exit code after %v = %d, want 0", sig, code)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("drain after %v did not complete in time", sig)
			}
			assertOneMessage(t, logs, "ControlHub shutdown drain complete; exiting")
		})
	}
}
