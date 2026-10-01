package service

// Tests for validatePGDSNBinding — the single PostgreSQL credential binding
// validator (G3, T2). WHY: every rejection below guards a concrete failure the
// binding layer must catch BEFORE pgx defaults or environment variables can
// silently complete a credential (e.g. PGUSER filling a missing user, default
// port 5432 filling a missing port, .pgpass filling an empty password). Accepted
// cases assert the effective *pgx.ConnConfig contents — the returned config is
// the exact config the executor will use, so RuntimeParams must only ever carry
// allowlisted server parameters.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		"host = db.internal port = 5432 dbname = orders user = ro password = pw sslmode=disable", // whitespace around '=' is legal libpq
		"host=db.internal\tport=5432\ndbname=orders user=ro password=pw sslmode=disable",         // ASCII whitespace separates pairs
		"host=db.internal port=5432 dbname=orders user=ro password=pw",                           // sslmode prefer -> same-endpoint TLS fallback is allowed
		"postgres://ro:pw@[::1]:5432/orders?sslmode=disable",
		"host=::1 port=5432 dbname=orders user=ro password=pw sslmode=disable",
		// libpq URI: connection fields may live entirely in query parameters.
		"postgresql:///orders?host=db.internal&port=5432&user=ro&password=pw&sslmode=disable",
		"host=db.internal port=5432 database=orders user=ro password=pw sslmode=disable", // pgx-internal spelling
		// Query parameters override the hierarchical part (driver precedence).
		"postgres://ro:pw@db.internal/orders?port=5432&sslmode=disable",
		"postgres://ignored:pw@db.internal:5432/orders?user=ro&sslmode=disable",
	} {
		wantHost := host
		if strings.Contains(dsn, "::1") {
			wantHost = "::1"
		}
		cfg, err := validatePGDSNBinding(dsn, wantHost, port, db)
		if err != nil {
			t.Fatalf("valid dsn rejected: %v\n  dsn: %s", err, dsn)
		}
		// The returned config IS the executed config — prove identity fields and
		// that no unexpected runtime parameters reached it.
		if cfg.Host != wantHost || int(cfg.Port) != port || cfg.Database != db {
			t.Fatalf("parsed config %v:%v/%v does not match %v:%v/%v", cfg.Host, cfg.Port, cfg.Database, wantHost, port, db)
		}
		if cfg.User == "" || cfg.Password == "" {
			t.Fatalf("accepted dsn produced empty user/password: user=%q", cfg.User)
		}
		for k := range cfg.RuntimeParams {
			if k != "application_name" {
				t.Fatalf("unexpected RuntimeParams key %q reached accepted config", k)
			}
		}
	}
}

// TestValidatePGDSNBinding_EffectiveConfigValues proves accepted DSNs land in the
// config fields they claim to: quoting, escapes, and application_name end up where
// libpq/pgx semantics say they must.
func TestValidatePGDSNBinding_EffectiveConfigValues(t *testing.T) {
	cfg, err := validatePGDSNBinding(
		"host=db.internal port=5432 dbname=orders user=ro password='p\\'w' application_name=probe sslmode=disable",
		"db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("valid dsn rejected: %v", err)
	}
	if cfg.Password != "p'w" {
		t.Fatalf("backslash-escaped quoted password decoded to %q, want %q", cfg.Password, "p'w")
	}
	if cfg.RuntimeParams["application_name"] != "probe" || len(cfg.RuntimeParams) != 1 {
		t.Fatalf("RuntimeParams = %v, want only application_name=probe", cfg.RuntimeParams)
	}
}

// TestValidatePGDSNBinding_AllowedKeysLandInRealFields proves every allowlisted
// connection option lands on the config field the driver actually consumes — the
// "parse once, execute this config" promise cannot hold if an option silently
// degrades into RuntimeParams.
func TestValidatePGDSNBinding_AllowedKeysLandInRealFields(t *testing.T) {
	cfg, err := validatePGDSNBinding(
		"host=db.internal port=5432 dbname=orders user=ro password=pw "+
			"sslmode=disable connect_timeout=7 application_name=ch "+
			"target_session_attrs=read-write krbsrvname=pg",
		"db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("valid dsn rejected: %v", err)
	}
	if cfg.ConnectTimeout != 7*time.Second {
		t.Fatalf("connect_timeout landed at %v, want 7s on Config.ConnectTimeout", cfg.ConnectTimeout)
	}
	if cfg.Config.TLSConfig != nil {
		t.Fatal("sslmode=disable must disable TLS, got non-nil TLSConfig")
	}
	if len(cfg.RuntimeParams) != 1 || cfg.RuntimeParams["application_name"] != "ch" {
		t.Fatalf("RuntimeParams = %v, want only application_name=ch", cfg.RuntimeParams)
	}
	// sslmode=prefer produces a same-endpoint TLS fallback — the only allowed
	// fallback shape (verified to be identical host+port).
	pref, err := validatePGDSNBinding(
		"host=db.internal port=5432 dbname=orders user=ro password=pw sslmode=prefer",
		"db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("sslmode=prefer rejected: %v", err)
	}
	for _, fb := range pref.Fallbacks {
		if fb.Host != pref.Host || fb.Port != pref.Port {
			t.Fatalf("prefer fallback is a different endpoint: %v:%v", fb.Host, fb.Port)
		}
	}
}

// TestValidatePGDSNBinding_URIDecodingSemantics proves the pre-check applies each
// URI decode exactly once, in pgconn order, so accepted configs carry the real
// password/database rather than a double-decoded or order-scrambled value.
func TestValidatePGDSNBinding_URIDecodingSemantics(t *testing.T) {
	for _, tc := range []struct {
		name         string
		dsn          string
		wantPassword string
	}{
		{"%25 decodes once to a literal percent", "postgres://ro:p%25ss@db.internal:5432/orders?sslmode=disable", "p%ss"},
		{"%252F decodes once to %2F", "postgres://ro:p%252Fss@db.internal:5432/orders?sslmode=disable", "p%2Fss"},
		{"raw plus stays a plus", "postgres://ro:p+q@db.internal:5432/orders?sslmode=disable", "p+q"},
		{"%2B decodes to plus", "postgres://ro:p%2Bss@db.internal:5432/orders?sslmode=disable", "p+ss"},
		{"percent password via query param", "postgres://ro@db.internal:5432/orders?password=p%25ss&sslmode=disable", "p%ss"},
	} {
		cfg, err := validatePGDSNBinding(tc.dsn, "db.internal", 5432, "orders")
		if err != nil {
			t.Fatalf("%s: valid dsn rejected: %v", tc.name, err)
		}
		if cfg.Password != tc.wantPassword {
			t.Fatalf("%s: cfg.Password decoded wrong (mismatch with the explicit value)", tc.name)
		}
	}
	// dbname/database aliases: last occurrence in raw order wins.
	cfg, err := validatePGDSNBinding(
		"postgres://ro:pw@db.internal:5432/other?database=&dbname=orders&sslmode=disable",
		"db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("dbname-last URI rejected: %v", err)
	}
	if cfg.Database != "orders" {
		t.Fatalf("dbname-last URI bound to %q, want orders", cfg.Database)
	}
	// A real backslash password (escaped \\ in keyword form) is preserved.
	cfg, err = validatePGDSNBinding(
		`host=db.internal port=5432 dbname=orders user=ro password=p\\w sslmode=disable`,
		"db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("escaped backslash password rejected: %v", err)
	}
	if cfg.Password != `p\w` {
		t.Fatalf("backslash password decoded wrong: %q", cfg.Password)
	}
}

// TestValidatePGDSNBinding_RejectedMatrix covers every rejection class. Rejected
// DSNs must not be able to reach a valid configuration through a second syntax,
// environment completion, or a passfile.
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
		{"empty userinfo password", "postgres://ro:@db.internal:5432/orders?sslmode=disable", host, port, db},
		{"empty user in userinfo", "postgres://:pw@db.internal:5432/orders", host, port, db},
		{"empty quoted password keyword", "host=db.internal port=5432 dbname=orders user=ro password=''", host, port, db},
		{"empty user keyword", "host=db.internal port=5432 dbname=orders user= password=pw", host, port, db},
		{"keyword missing password", "host=db.internal port=5432 dbname=orders user=ro", host, port, db},
		{"keyword missing port", "host=db.internal dbname=orders user=u password=p", host, port, db},
		{"keyword missing dbname", "host=db.internal port=5432 user=u password=p", host, port, db},
		// A superseding empty query value clears the field — it must not fall
		// back to env/defaults.
		{"query empties password", "postgres://ro:pw@db.internal:5432/orders?password=", host, port, db},
		{"query empties host", "postgres://ro:pw@db.internal:5432/orders?host=", host, port, db},
		{"query empties port", "postgres://ro:pw@db.internal:5432/orders?port=", host, port, db},
		{"query empties user", "postgres://ro:pw@db.internal:5432/orders?user=", host, port, db},
		{"query empties dbname", "postgres://ro:pw@db.internal:5432/orders?dbname=", host, port, db},
		{"multi-host URI", "postgres://ro:pw@h1:111,h2:222/orders", host, port, db},
		{"multi-host via percent-encoded comma", "postgres://ro:pw@db.internal%2Cevil:5432/orders", host, port, db},
		{"multi-host via query", "postgres://ro:pw@db.internal:5432/orders?host=a,b", host, port, db},
		{"multi-host keyword", "host=h1,h2 port=5432 dbname=orders user=u password=p", host, port, db},
		{"multi-port keyword", "host=db.internal port=5432,5433 dbname=orders user=u password=p", host, port, db},
		// A trailing backslash escapes nothing — pgx drops it, leaving an empty
		// password a passfile could then fill.
		{"trailing backslash empties password", `host=db.internal port=5432 dbname=orders user=ro password=\`, host, port, db},
		// Raw ASCII space at a value's edge is trimmed by uriDecode — the
		// effective password is empty even though the text is non-empty.
		{"raw-space-only password", "postgres://ro:pw@db.internal:5432/orders?sslmode=disable&password= ", host, port, db},
		{"raw space inside query value", "postgres://ro:pw@db.internal:5432/orders?password=p w", host, port, db},
		// Alias keys canonicalize; the LAST occurrence in raw order wins — an
		// empty "database=" last must not be masked by an earlier dbname, and a
		// real "dbname=" last must not be cleared by an earlier empty database.
		{"empty database= after dbname clears it", "postgres://ro:pw@db.internal:5432/orders?dbname=x&database=", host, port, db},
		{"options= can mutate session GUCs", "postgres://ro:pw@db.internal:5432/orders?options=-c%20statement_timeout%3D1s", host, port, db},
		{"options= keyword form", "host=db.internal port=5432 dbname=orders user=u password=p options='-c x'", host, port, db},
		// Review P1: double quotes are ordinary characters under libpq — this DSN
		// hides a real `options` key a naive lexer that treats '"' as quoting
		// would swallow inside the password. The driver's own parser must catch it.
		{"double-quote smuggles options key", `host=db.internal port=5432 dbname=orders user=ro password="p options='-c statement_timeout=0' application_name=probe" sslmode=disable`, host, port, db},
		// Non-space ASCII whitespace is a real separator — a forbidden key after
		// a tab or newline cannot hide inside a neighboring value.
		{"tab-separated options key", "host=db.internal\tport=5432\tdbname=orders\tuser=ro\tpassword=pw\toptions='-c x'", host, port, db},
		{"newline-separated options key", "host=db.internal port=5432 dbname=orders user=ro password=pw\noptions='-c x'", host, port, db},
		{"service= re-sources config", "host=db.internal port=5432 dbname=orders user=u password=p service=mysvc", host, port, db},
		{"passfile= re-sources secrets", "host=db.internal port=5432 dbname=orders user=u password=p passfile=/tmp/x", host, port, db},
		{"sslpassword= not allowlisted", "host=db.internal port=5432 dbname=orders user=u password=p sslpassword=x", host, port, db},
		{"sslnegotiation= not allowlisted", "host=db.internal port=5432 dbname=orders user=u password=p sslnegotiation=postgres", host, port, db},
		{"channel_binding= not allowlisted", "host=db.internal port=5432 dbname=orders user=u password=p channel_binding=require", host, port, db},
		{"require_auth= not allowlisted", "host=db.internal port=5432 dbname=orders user=u password=p require_auth=password", host, port, db},
		{"krbspn= not allowlisted", "host=db.internal port=5432 dbname=orders user=u password=p krbspn=x", host, port, db},
		{"unknown key rejected", "postgres://ro:pw@db.internal:5432/orders?sslfactory=x", host, port, db},
		// libpq client options pgx does not consume — they would silently land in
		// RuntimeParams and be sent to the server. Controlled rejection (compat
		// gap vs the spec's libpq-spelled allowlist, tracked in #108).
		{"keepalives not consumed by pgx", "host=db.internal port=5432 dbname=orders user=u password=p keepalives=1", host, port, db},
		{"keepalives_idle not consumed", "host=db.internal port=5432 dbname=orders user=u password=p keepalives_idle=30", host, port, db},
		{"keepalives_interval not consumed", "host=db.internal port=5432 dbname=orders user=u password=p keepalives_interval=5", host, port, db},
		{"keepalives_count not consumed", "host=db.internal port=5432 dbname=orders user=u password=p keepalives_count=3", host, port, db},
		{"tcp_user_timeout not consumed", "host=db.internal port=5432 dbname=orders user=u password=p tcp_user_timeout=30000", host, port, db},
		{"gssencmode not consumed", "host=db.internal port=5432 dbname=orders user=u password=p gssencmode=prefer", host, port, db},
		{"requiressl not consumed", "host=db.internal port=5432 dbname=orders user=u password=p requiressl=1", host, port, db},
		{"sslcrl not consumed", "host=db.internal port=5432 dbname=orders user=u password=p sslcrl=/x", host, port, db},
		{"sslcrldir not consumed", "host=db.internal port=5432 dbname=orders user=u password=p sslcrldir=/x", host, port, db},
		{"ssl_min_protocol_version not consumed", "host=db.internal port=5432 dbname=orders user=u password=p ssl_min_protocol_version=TLSv1.2", host, port, db},
		{"ssl_max_protocol_version not consumed", "host=db.internal port=5432 dbname=orders user=u password=p ssl_max_protocol_version=TLSv1.3", host, port, db},
		{"min_protocol_version is a wire-protocol knob, not in the allowlist", "host=db.internal port=5432 dbname=orders user=u password=p min_protocol_version=3.2", host, port, db},
		{"keepalives in URI form", "postgres://ro:pw@db.internal:5432/orders?keepalives=1", host, port, db},
		{"unix socket host", "host=/tmp port=5432 dbname=orders user=u password=p", host, port, db},
		{"unix socket via query host", "postgres://ro:pw@db.internal:5432/orders?host=%2Ftmp", host, port, db},
		{"IPv6 literal != hostname", "postgres://ro:pw@[::1]:5432/orders", host, port, db},
		{"IPv6 missing port", "postgres://ro:pw@[::1]/orders", "::1", port, db},
		{"non-numeric port", "host=db.internal port=abc dbname=orders user=u password=p", host, port, db},
		{"port 0", "host=db.internal port=0 dbname=orders user=u password=p", host, port, db},
		{"trailing bare token", "postgres://ro:pw@db.internal:5432/orders port=9999", host, port, db},
		{"empty dsn", "", host, port, db},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validatePGDSNBinding(tc.dsn, tc.h, tc.p, tc.d); err == nil {
				t.Fatalf("dsn was NOT rejected: %s", tc.dsn)
			}
		})
	}
}

// TestValidatePGDSNBinding_EnvironmentCannotComplete proves the explicitness rule
// holds even when the environment COULD fill the gap: PGUSER/PGPASSWORD and a
// matching passfile entry must never satisfy a binding field.
func TestValidatePGDSNBinding_EnvironmentCannotComplete(t *testing.T) {
	t.Setenv("PGUSER", "envuser")
	t.Setenv("PGPASSWORD", "envpw")
	t.Setenv("PGPORT", "5432")
	passfile := filepath.Join(t.TempDir(), "pgpass")
	if err := os.WriteFile(passfile, []byte("db.internal:5432:orders:ro:frompassfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PGPASSFILE", passfile)

	for _, dsn := range []string{
		"host=db.internal port=5432 dbname=orders",                                    // env would fill user+password
		"host=db.internal port=5432 dbname=orders user=ro password=pw user=",          // empty user cannot be re-emptied
		"postgres://ro@db.internal:5432/orders",                                       // passfile could fill the password
		"postgres://ro:@db.internal:5432/orders?sslmode=disable",                      // explicit-empty password likewise
		"postgres://ro:pw@db.internal:5432/orders?password=",                          // emptied by query param
		`host=db.internal port=5432 dbname=orders user=ro sslmode=disable password=\`, // trailing backslash decodes to empty
		"postgres://ro:pw@db.internal:5432/orders?password= ",                         // raw-space trims to empty
		"postgres://db.internal:5432/orders?password=pw&sslmode=disable",              // env would fill user
	} {
		if _, err := validatePGDSNBinding(dsn, "db.internal", 5432, "orders"); err == nil {
			t.Fatalf("dsn reached config via environment/passfile completion: %s", dsn)
		}
	}
}
