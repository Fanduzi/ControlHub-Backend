package service

// Tests for validatePGDSNBinding — the single PostgreSQL credential binding
// validator (G3, T2). WHY: every rejection below guards a concrete failure the
// binding layer must catch BEFORE pgx defaults or environment variables can
// silently complete a credential (e.g. PGUSER filling a missing user, default
// port 5432 filling a missing port). The accepted config must be the exact
// *pgx.ConnConfig the executor uses — parse once, use everywhere.

import (
	"strings"
	"testing"
)

func TestValidatePGDSNBinding_AcceptedForms(t *testing.T) {
	const (
		host = "db.internal"
		port = 5432
		db   = "orders"
	)
	for _, dsn := range []string{
		"postgres://ro:pw@db.internal:5432/orders?sslmode=disable",
		"postgresql://ro:pw@db.internal:5432/orders?sslmode=disable&connect_timeout=5&application_name=ch",
		"host=db.internal port=5432 dbname=orders user=ro password=pw sslmode=disable",
		"host=db.internal port=5432 dbname=orders user=ro password='p w' sslmode=disable",
		"host=db.internal port=5432 dbname=orders user=ro password=pw", // sslmode prefer -> same-endpoint TLS fallback is allowed
		"postgres://ro:pw@[::1]:5432/orders?sslmode=disable",
		"host=::1 port=5432 dbname=orders user=ro password=pw sslmode=disable",
	} {
		wantHost := host
		if strings.Contains(dsn, "::1") {
			wantHost = "::1"
		}
		cfg, err := validatePGDSNBinding(dsn, wantHost, port, db)
		if err != nil {
			t.Fatalf("valid dsn rejected: %v\n  dsn: %s", err, dsn)
		}
		// The returned config IS the executed config — spot-check identity fields.
		if !strings.EqualFold(cfg.Host, wantHost) || int(cfg.Port) != port || cfg.Database != db || cfg.User == "" {
			t.Fatalf("parsed config %v:%v/%v user=%q does not match %v:%v/%v", cfg.Host, cfg.Port, cfg.Database, cfg.User, wantHost, port, db)
		}
	}
}

func TestValidatePGDSNBinding_RejectedMatrix(t *testing.T) {
	const (
		host = "db.internal"
		port = 5432
		db   = "orders"
	)
	for _, tc := range []struct {
		name string
		dsn  string
		h    string
		p    int
		d    string
	}{
		{"host mismatch", "postgres://ro:pw@db.internal:5432/orders?sslmode=disable", "other", port, db},
		{"port mismatch", "postgres://ro:pw@db.internal:5555/orders", host, port, db},
		{"database mismatch (the MySQL-era gap)", "postgres://ro:pw@db.internal:5432/orders_other", host, port, db},
		{"missing port (pgx would default 5432)", "postgres://ro:pw@db.internal/orders", host, port, db},
		{"missing database (pgx would default to user)", "postgres://ro:pw@db.internal:5432/", host, port, db},
		{"missing user", "postgres://db.internal:5432/orders", host, port, db},
		{"missing password", "postgres://ro@db.internal:5432/orders", host, port, db},
		{"keyword missing password", "host=db.internal port=5432 dbname=orders user=ro", host, port, db},
		{"keyword missing port", "host=db.internal dbname=orders user=u password=p", host, port, db},
		{"keyword missing dbname", "host=db.internal port=5432 user=u password=p", host, port, db},
		{"multi-host URI", "postgres://ro:pw@h1:111,h2:222/orders", host, port, db},
		{"multi-host keyword", "host=h1,h2 port=5432 dbname=orders user=u password=p", host, port, db},
		{"multi-port keyword", "host=db.internal port=5432,5433 dbname=orders user=u password=p", host, port, db},
		{"options= can mutate session GUCs", "postgres://ro:pw@db.internal:5432/orders?options=-c%20statement_timeout%3D1s", host, port, db},
		{"service= re-sources config", "host=db.internal port=5432 dbname=orders user=u password=p service=mysvc", host, port, db},
		{"passfile= re-sources secrets", "host=db.internal port=5432 dbname=orders user=u password=p passfile=/tmp/x", host, port, db},
		{"unknown key rejected", "postgres://ro:pw@db.internal:5432/orders?sslfactory=x", host, port, db},
		{"unix socket host", "host=/tmp port=5432 dbname=orders user=u password=p", host, port, db},
		{"IPv6 literal != hostname", "postgres://ro:pw@[::1]:5432/orders", host, port, db},
		{"IPv6 missing port", "postgres://ro:pw@[::1]/orders", "::1", port, db},
		{"non-numeric port", "host=db.internal port=abc dbname=orders user=u password=p", host, port, db},
		{"port 0", "host=db.internal port=0 dbname=orders user=u password=p", host, port, db},
		{"trailing bare token", "postgres://ro:pw@db.internal:5432/orders port=9999", host, port, db},
		{"empty dsn", "", host, port, db},
		// Environment/defaults can never fill a binding field: this DSN parses
		// fine under pgx alone (PGUSER supplies the user), but must be rejected.
		{"env-defaulted user/password", "host=db.internal port=5432 dbname=orders", host, port, db},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validatePGDSNBinding(tc.dsn, tc.h, tc.p, tc.d); err == nil {
				t.Fatalf("dsn was NOT rejected: %s", tc.dsn)
			}
		})
	}
}
