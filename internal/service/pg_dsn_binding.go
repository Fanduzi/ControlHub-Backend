// Package service provides the PostgreSQL credential DSN binding validator.
// input: fmt, net/url, strconv, strings, jackc/pgx/v5, internal/model
// output: validatePGDSNBinding — single parse-once-use-everywhere entry returning the executable *pgx.ConnConfig
// pos: PG workbench T2 (G3) — validates that a resolved PostgreSQL DSN explicitly and exactly binds to the selected connection (host, port, dbname) before any connection is built
// note: if this file changes, update header and README.md
package service

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
)

// errPGDSNBinding is the fail-closed sentinel for every PostgreSQL DSN binding
// rejection. Callers map it to a fixed status/message — the wrapped reasons are
// static strings that never echo the DSN, password, or resolved endpoint.
var errPGDSNBinding = fmt.Errorf("credential DSN does not bind to the connection")

// allowedPGDSNKeys is the allowlist for URI query params and keyword DSN keys.
// Anything outside this list is rejected — fail closed rather than ignore a
// knob that changes where/how the connection lands (e.g. options= can mutate
// session GUCs; service=/passfile= re-source config or secrets).
var allowedPGDSNKeys = map[string]bool{
	"user": true, "password": true, "host": true, "port": true, "dbname": true,
	"sslmode": true, "sslcert": true, "sslkey": true, "sslrootcert": true,
	"sslcrl": true, "sslcrldir": true, "sslsni": true,
	"ssl_min_protocol_version": true, "ssl_max_protocol_version": true,
	"connect_timeout": true, "application_name": true,
	"target_session_attrs": true,
	"keepalives":           true, "keepalives_idle": true, "keepalives_interval": true,
	"keepalives_count": true, "tcp_user_timeout": true,
	"gssencmode": true, "krbsrvname": true,
	"requiressl": true, // deprecated libpq alias; honored by pgconn
}

// validatePGDSNBinding parses dsn with the real pgx parser AND verifies the raw
// text carries every binding-relevant field explicitly — user, password, host,
// port, dbname must appear in the DSN itself; environment/driver defaults
// (PGUSER, default port 5432, dbname=user) must never silently complete a
// binding field. The parsed config must bind exactly to the connection's
// host/port/database. The returned *pgx.ConnConfig is the config the executor
// must use — parse once, use everywhere; the DSN is never stored or logged.
func validatePGDSNBinding(dsn, wantHost string, wantPort int, wantDB string) (*pgx.ConnConfig, error) {
	raw := strings.TrimSpace(dsn)
	if raw == "" {
		return nil, fmt.Errorf("%w: empty dsn", errPGDSNBinding)
	}

	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		if err := checkPGURIForm(raw); err != nil {
			return nil, err
		}
	} else {
		if err := checkPGKeywordForm(raw); err != nil {
			return nil, err
		}
	}

	cfg, err := pgx.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: unparsable dsn", errPGDSNBinding)
	}
	// pgx adds a same-endpoint TLS-mode fallback whenever sslmode allows a retry
	// (e.g. prefer) — that is NOT a multi-host config. A different host or port
	// in any fallback is what must be rejected.
	for _, fb := range cfg.Fallbacks {
		if !strings.EqualFold(fb.Host, cfg.Host) || fb.Port != cfg.Port {
			return nil, fmt.Errorf("%w: multi-host/fallback endpoints are not supported", errPGDSNBinding)
		}
	}
	if cfg.Host == "" || strings.HasPrefix(cfg.Host, "/") {
		return nil, fmt.Errorf("%w: unix-socket hosts are not supported", errPGDSNBinding)
	}
	if !strings.EqualFold(cfg.Host, wantHost) || int(cfg.Port) != wantPort || cfg.Database != wantDB {
		return nil, fmt.Errorf("%w: dsn endpoint/database does not match the connection", errPGDSNBinding)
	}
	return cfg, nil
}

// validatePGDSNBindingForTarget is the target-shaped convenience wrapper: it
// binds the resolved DSN to the target's host/port and to the connection row's
// database_name — the dbname must equal the addressed connection, which is the
// core fix over the MySQL-era host/port-only binding.
func validatePGDSNBindingForTarget(dsn string, target model.QueryTarget, databaseName string) (*pgx.ConnConfig, error) {
	return validatePGDSNBinding(dsn, target.ConnectionContext.Host, target.ConnectionContext.Port, databaseName)
}

// checkPGURIForm verifies explicitness on the raw URI (net/url parse) —
// independent of pgx so no default can silently fill a binding field.
func checkPGURIForm(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: unparsable URI", errPGDSNBinding)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: missing host", errPGDSNBinding)
	}
	if strings.Contains(u.Host, ",") {
		return fmt.Errorf("%w: multi-host authority is not supported", errPGDSNBinding)
	}
	port := u.Port()
	if port == "" {
		return fmt.Errorf("%w: missing explicit port", errPGDSNBinding)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("%w: invalid port", errPGDSNBinding)
	}
	dbname, err := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), "/"))
	if err != nil || dbname == "" || strings.Contains(dbname, "/") {
		return fmt.Errorf("%w: missing database name", errPGDSNBinding)
	}
	if u.User == nil || u.User.Username() == "" {
		return fmt.Errorf("%w: missing user", errPGDSNBinding)
	}
	_, hasPw := u.User.Password()
	q := u.Query()
	if !hasPw && q.Get("password") == "" {
		return fmt.Errorf("%w: missing password", errPGDSNBinding)
	}
	for k := range q {
		if !allowedPGDSNKeys[strings.ToLower(k)] {
			return fmt.Errorf("%w: dsn key is not allowed", errPGDSNBinding)
		}
	}
	return nil
}

// checkPGKeywordForm tokenizes key=value pairs honoring single/double quotes
// and backslash escapes — the libpq conninfo syntax.
func checkPGKeywordForm(raw string) error {
	kv, err := parsePGConnInfo(raw)
	if err != nil {
		return err
	}
	for _, k := range []string{"user", "password", "host", "port", "dbname"} {
		if v, ok := kv[k]; !ok || v == "" {
			return fmt.Errorf("%w: missing %s=", errPGDSNBinding, k)
		}
	}
	if strings.Contains(kv["host"], ",") || strings.Contains(kv["port"], ",") {
		return fmt.Errorf("%w: multi-host/port lists are not supported", errPGDSNBinding)
	}
	if strings.HasPrefix(kv["host"], "/") {
		return fmt.Errorf("%w: unix-socket hosts are not supported", errPGDSNBinding)
	}
	if p, err := strconv.Atoi(kv["port"]); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("%w: invalid port", errPGDSNBinding)
	}
	for k := range kv {
		if !allowedPGDSNKeys[k] {
			return fmt.Errorf("%w: dsn key is not allowed", errPGDSNBinding)
		}
	}
	return nil
}

// parsePGConnInfo splits `k=v k='v v' k="v"` pairs. Keys are lowercased; a bare
// token or missing '=' is a hard error — the DSN is admin-controlled config
// and must fail loudly, not be reinterpreted.
func parsePGConnInfo(s string) (map[string]string, error) {
	kv := map[string]string{}
	i, n := 0, len(s)
	for i < n {
		for i < n && s[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && s[i] != '=' && s[i] != ' ' {
			i++
		}
		key := strings.ToLower(s[start:i])
		if key == "" || i >= n || s[i] != '=' {
			return nil, fmt.Errorf("%w: malformed conninfo", errPGDSNBinding)
		}
		i++ // '='
		var v strings.Builder
		if i < n && (s[i] == '\'' || s[i] == '"') {
			quote := s[i]
			i++
			for i < n && s[i] != quote {
				if s[i] == '\\' && i+1 < n {
					i++
				}
				v.WriteByte(s[i])
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("%w: unterminated quoted value for %s=", errPGDSNBinding, key)
			}
			i++
		} else {
			for i < n && s[i] != ' ' {
				if s[i] == '\\' && i+1 < n {
					i++
				}
				v.WriteByte(s[i])
				i++
			}
		}
		if _, dup := kv[key]; dup {
			return nil, fmt.Errorf("%w: duplicate key %s=", errPGDSNBinding, key)
		}
		kv[key] = v.String()
	}
	if len(kv) == 0 {
		return nil, fmt.Errorf("%w: not a keyword/value dsn", errPGDSNBinding)
	}
	return kv, nil
}
