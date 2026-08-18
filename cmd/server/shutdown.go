// Package main provides the ControlHub application entry point.
// input: context, errors, net, net/http, os, time
// output: shutdownDrainTimeout, runServer, drainAndExit
// pos: HTTP server lifecycle seam — serves until a SIGTERM/SIGINT shutdown signal, then stops new traffic and drains in-flight handlers for at most the fixed ten-second bound (Issue #37); a clean drain exits 0, and bound exhaustion, server failure, or a second signal exits 1 after exactly one fixed safe log
// note: if the shutdown contract (bound, exit codes, log wording, forced-second-signal behavior) changes, update this file, main.go's header, and cmd/server/README.md
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"time"
)

// shutdownDrainTimeout is the fixed maximum bound for draining in-flight
// handlers after SIGTERM/SIGINT. It is a product invariant, not environment
// configuration: the existing five-second query deadline plus the two-second
// Evidence Persistence Window plus scheduling margin (Issue #37).
const shutdownDrainTimeout = 10 * time.Second

// runServer serves ln until signals delivers a SIGTERM/SIGINT shutdown
// signal, then stops new traffic and drains in-flight handlers for at most
// drain. drain == 0 selects the fixed shutdownDrainTimeout; tests inject a
// short bound. A clean drain returns 0. Shutdown bound exhaustion, an HTTP
// server failure, or a second signal returns 1. logf receives exactly one
// fixed safe message per terminal outcome — never error values, request data,
// or DSNs — so tests can assert the shutdown log contract.
func runServer(srv *http.Server, ln net.Listener, signals <-chan os.Signal, drain time.Duration, logf func(string, ...any)) int {
	if drain == 0 {
		drain = shutdownDrainTimeout
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return 0
		}
		logf("ControlHub http server failed; exiting")
		return 1
	case <-signals:
		return drainAndExit(srv, signals, drain, logf)
	}
}

// drainAndExit runs http.Server.Shutdown under a drain bound and returns the
// process exit code: 0 for a clean drain, 1 when the bound is exhausted
// (after force-releasing any remaining connections; the exit code carries the
// failure). Shutdown never cancels in-flight request contexts, so handlers —
// including a governed query's five-second deadline and its two-second
// Evidence Persistence Window — can finish during the drain. A second signal
// during the drain forces immediate exit. Exactly one fixed safe message is
// logged per outcome.
func drainAndExit(srv *http.Server, signals <-chan os.Signal, drain time.Duration, logf func(string, ...any)) int {
	ctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- srv.Shutdown(ctx) }()

	select {
	case <-signals:
		logf("ControlHub second shutdown signal; exiting immediately")
		return 1
	case err := <-done:
		if err != nil {
			_ = srv.Close()
			logf("ControlHub shutdown drain deadline exceeded; exiting")
			return 1
		}
	}
	logf("ControlHub shutdown drain complete; exiting")
	return 0
}
