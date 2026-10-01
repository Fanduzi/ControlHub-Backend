// Package service — pg_dsn_binding.go: PostgreSQL DSN binding validation for governed
// query credentials. The DSN itself is never persisted, logged, or returned; only its
// pass/fail verdict and parsed *pgx.ConnConfig flow onward.
//
// Contract (per frozen spec G3):
//   - required fields (user, password, host, port, dbname) must be explicit in the
//     raw text and non-empty — environment defaults or driver fallback must not fill
//     them (PGUSER/PGPASSWORD/PGHOST/PGPORT/PGDATABASE/PGSERVICE/.pgpass);
//   - the key space is an explicit allowlist enforced twice: once by the driver's own
//     parser (pgconn ConnStringAllowedKeys, covering both keyword and URI forms
//     including superseded repeated keys) and once on the effective RuntimeParams so
//     libpq-only client options cannot smuggle session mutations to the server;
//   - forbidden options (options, service, passfile, sslpassword, sslnegotiation,
//     channel_binding, require_auth, krbspn, servicefile, and any unknown key) are
//     rejected outright;
//   - multi-host / multi-port and alternate fallback endpoints are rejected;
//   - unix-socket hosts are rejected;
//   - effective host/port/database must bind to the target's connection context and
//     the credential row's database_name.
//
// pgx v5.11.0 compatibility note (deviation from the spec's libpq-spelled allowlist,
// reported under issue #108): sslcrl, sslcrldir, ssl_min_protocol_version,
// ssl_max_protocol_version, keepalives, keepalives_idle, keepalives_interval,
// keepalives_count, tcp_user_timeout, gssencmode, and requiressl are NOT consumed by
// pgconn — they would silently become RuntimeParams sent to the server, so they are
// rejected here rather than honored. application_name is the single allowed
// RuntimeParams key.
package service

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fan/controlhub/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// allowedPGDSNKeys is the ConnStringAllowedKeys list handed to pgx: keys pgconn
// actually consumes as connection configuration plus application_name (a legitimate
// server runtime parameter). Keys the driver would only forward into RuntimeParams
// are deliberately absent — see the package contract note above.
var allowedPGDSNKeys = []string{
	"user",
	"password",
	"host",
	"port",
	"dbname", // pgx also accepts its internal spelling "database"
	"sslmode",
	"sslcert",
	"sslkey",
	"sslrootcert",
	"sslsni",
	"connect_timeout",
	"application_name",
	"target_session_attrs",
	"krbsrvname",
}

// allowedPGRuntimeParams is the closed set of effective RuntimeParams keys a bound
// DSN may produce. Everything else — whether smuggled through the allowlist or
// introduced by a future driver change — is rejected after parsing.
var allowedPGRuntimeParams = map[string]struct{}{
	"application_name": {},
}

// pgConnInfoSpace mirrors the ASCII whitespace class libpq/pgx use between
// keyword=value pairs (space, tab, newline, vertical tab, form feed, carriage
// return). Double quotes are ordinary value characters — only single quotes quote.
const pgConnInfoSpace = " \t\n\v\f\r"

// parsePGConnInfo lexes a libpq keyword/value string exactly as pgconn does:
// whitespace may surround keys and '=', single-quoted values support backslash
// escapes (as do unquoted values), and a value ends at ASCII whitespace only.
func parsePGConnInfo(dsn string) (map[string]string, error) {
	out := make(map[string]string)
	s := strings.TrimLeft(dsn, pgConnInfoSpace)
	for len(s) > 0 {
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			return nil, fmt.Errorf("invalid keyword/value in DSN")
		}
		key := strings.Trim(s[:eq], pgConnInfoSpace)
		if key == "" || strings.ContainsAny(key, pgConnInfoSpace) {
			return nil, fmt.Errorf("invalid keyword in DSN")
		}
		s = strings.TrimLeft(s[eq+1:], pgConnInfoSpace)
		var val string
		if len(s) > 0 && s[0] == '\'' {
			s = s[1:]
			end := 0
			for ; end < len(s); end++ {
				if s[end] == '\'' {
					break
				}
				if s[end] == '\\' {
					end++
					if end == len(s) {
						return nil, fmt.Errorf("unterminated quoted string in DSN")
					}
				}
			}
			if end == len(s) {
				return nil, fmt.Errorf("unterminated quoted string in DSN")
			}
			val = unescapePGKeywordValue(s[:end])
			s = strings.TrimLeft(s[end+1:], pgConnInfoSpace)
		} else {
			end := 0
			for ; end < len(s); end++ {
				if strings.IndexByte(pgConnInfoSpace, s[end]) >= 0 {
					break
				}
				if s[end] == '\\' {
					end++
					if end == len(s) {
						break
					}
				}
			}
			val = unescapePGKeywordValue(s[:end])
			s = strings.TrimLeft(s[end:], pgConnInfoSpace)
		}
		// pgconn canonicalizes dbname -> database; do the same so that a later
		// "database=" spelling correctly supersedes an earlier "dbname=".
		if key == "dbname" {
			key = "database"
		}
		out[key] = val
	}
	return out, nil
}

// unescapePGKeywordValue applies libpq's rule: a backslash is dropped and the next
// character is taken literally; a trailing backslash contributes nothing.
func unescapePGKeywordValue(raw string) string {
	if !strings.ContainsRune(raw, '\\') {
		return raw
	}
	var sb strings.Builder
	sb.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
		}
		sb.WriteByte(raw[i])
	}
	return sb.String()
}

// pgURIExplicitFields resolves the effective per-field text a URI supplies, using
// pgconn's own precedence: query parameters override the hierarchical part, and an
// empty userinfo password is treated as absent (pgconn drops it, which would let a
// passfile fill it). Returned values are percent-decoded.
func pgURIExplicitFields(u *url.URL) (map[string]string, error) {
	q := u.Query()
	last := func(key string) (string, bool) {
		vals, ok := q[key]
		if !ok {
			return "", false
		}
		return vals[len(vals)-1], true
	}
	decode := func(raw string) (string, error) {
		v, err := url.PathUnescape(raw)
		if err != nil {
			return "", fmt.Errorf("invalid percent-encoding in DSN")
		}
		return v, nil
	}

	fields := make(map[string]string)
	// userinfo: "user" / "user:password" before '@'; empty components are absent.
	username := ""
	password := ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	if v, err := decode(username); err != nil {
		return nil, err
	} else {
		fields["user"] = v
	}
	if v, err := decode(password); err != nil {
		return nil, err
	} else {
		fields["password"] = v
	}
	// A multi-host authority is not valid net/url — pgconn parses it manually —
	// so detect the comma on the raw authority before splitting host/port.
	rawHost := u.Host
	if i := strings.IndexByte(rawHost, ':'); i >= 0 && !strings.HasPrefix(rawHost, "[") {
		rawHost = rawHost[:i]
	}
	if strings.HasPrefix(rawHost, "[") {
		if end := strings.IndexByte(rawHost, ']'); end >= 0 {
			rawHost = rawHost[1:end]
		}
	}
	if v, err := decode(rawHost); err != nil {
		return nil, err
	} else {
		fields["host"] = v
	}
	fields["port"] = u.Port()
	// Path supplies dbname; the whole path segment (sans '?') is the dbname.
	dbname := strings.TrimPrefix(u.EscapedPath(), "/")
	if i := strings.IndexByte(dbname, '?'); i >= 0 {
		dbname = dbname[:i]
	}
	if v, err := decode(dbname); err != nil {
		return nil, err
	} else {
		fields["database"] = v
	}
	// Query parameters override every hierarchical field; presence of the key
	// means override even when its value is empty.
	for _, key := range []string{"user", "password", "host", "port", "dbname", "database"} {
		if v, ok := last(key); ok {
			fields[pgCanonicalField(key)] = v
		}
	}
	return fields, nil
}

// pgCanonicalField maps either accepted spelling to the canonical settings key
// pgconn uses internally.
func pgCanonicalField(key string) string {
	if key == "dbname" {
		return "database"
	}
	return key
}

// pgExplicitFields returns the field→text map for whichever DSN form the input is,
// so required-field explicitness can be checked before driver parsing.
func pgExplicitFields(dsn string) (map[string]string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid PostgreSQL DSN")
		}
		return pgURIExplicitFields(u)
	}
	return parsePGConnInfo(dsn)
}

// validatePGDSNBindingForTarget binds a DSN to a target's connection context —
// the seam credential inspection and dev seeding share.
func validatePGDSNBindingForTarget(dsn string, target model.QueryTarget, databaseName string) (*pgx.ConnConfig, error) {
	cc := target.ConnectionContext
	return validatePGDSNBinding(dsn, cc.Host, cc.Port, databaseName)
}

// validatePGDSNBinding parses the DSN once and proves it binds to the expected
// host/port/database. The returned config is the only parsed representation — callers
// must reuse it rather than re-parsing.
func validatePGDSNBinding(dsn, host string, port int, databaseName string) (*pgx.ConnConfig, error) {
	// 1. Required fields must be explicit and non-empty in the raw text. This runs
	//    before any driver parse so environment variables, defaults, or passfile can
	//    never satisfy them.
	fields, err := pgExplicitFields(dsn)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"user", "password", "host", "port", "database"} {
		if fields[key] == "" {
			return nil, fmt.Errorf("PostgreSQL DSN must specify %s explicitly (environment/default completion is forbidden)", key)
		}
	}
	if strings.Contains(fields["host"], ",") || strings.Contains(fields["port"], ",") {
		return nil, fmt.Errorf("PostgreSQL DSN must address a single host and port")
	}
	if strings.HasPrefix(fields["host"], "/") {
		return nil, fmt.Errorf("unix-socket DSN hosts are not permitted")
	}
	if strings.HasPrefix(fields["host"], "@") {
		return nil, fmt.Errorf("abstract unix-socket DSN hosts are not permitted")
	}
	if p, err := strconv.Atoi(fields["port"]); err != nil || p < 1 || p > 65535 {
		return nil, fmt.Errorf("PostgreSQL DSN port must be a valid TCP port number")
	}

	// 2. Parse once with the driver's own allowed-key restriction: any conninfo key
	//    outside the allowlist — keyword or URI form, including superseded repeats —
	//    fails here before filesystem or network access.
	cfg, err := pgx.ParseConfigWithOptions(dsn, pgx.ParseConfigOptions{
		ParseConfigOptions: pgconn.ParseConfigOptions{ConnStringAllowedKeys: allowedPGDSNKeys},
	})
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL DSN")
	}

	// 3. Effective-config checks: nothing may reach the server that the allowlist
	//    did not intend, and every fallback must be the same endpoint (e.g.
	//    sslmode=prefer) — never an alternate host or port.
	for k := range cfg.RuntimeParams {
		if _, ok := allowedPGRuntimeParams[k]; !ok {
			return nil, fmt.Errorf("PostgreSQL DSN uses a connection option the driver cannot honor safely")
		}
	}
	if strings.HasPrefix(cfg.Host, "/") || strings.HasPrefix(cfg.Host, "@") {
		return nil, fmt.Errorf("unix-socket DSN hosts are not permitted")
	}
	for _, fb := range cfg.Fallbacks {
		if fb.Host != cfg.Host || fb.Port != cfg.Port {
			return nil, fmt.Errorf("PostgreSQL DSN must address a single endpoint")
		}
	}
	if cfg.Host != host {
		return nil, fmt.Errorf("PostgreSQL DSN host does not match the target connection endpoint")
	}
	if int(cfg.Port) != port {
		return nil, fmt.Errorf("PostgreSQL DSN port does not match the target connection endpoint")
	}
	if cfg.Database != databaseName {
		return nil, fmt.Errorf("PostgreSQL DSN dbname does not match the credential connection identity")
	}
	return cfg, nil
}
