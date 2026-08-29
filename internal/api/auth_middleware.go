// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, net/http, strings, time, internal/service
// output: MaxQueryTokenAge, user-only auth/role/query-freshness middleware, actorUserIDFromContext, actorRoleFromContext
// pos: User Bearer boundary — signature/current Authorization Version checks and explicit machine-credential rejection prevent user/session fallback; untrusted Bearer rejection persistence is bounded by the router-wired per-process budget
// note: if this file changes, update this header and module README.md.
package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fan/controlhub/internal/service"
)

// actorContextKey is the unexported context key for the authenticated actor's
// user id. An empty-struct key avoids collisions with other packages.
type actorContextKey struct{}

// actorRoleContextKey is the unexported context key for the authenticated
// actor's current server-owned role. Handlers read it for role gates; the role
// is loaded during VerifyToken from durable authorization state, never from a
// client-supplied claim alone.
type actorRoleContextKey struct{}

// MaxQueryTokenAge is the fixed eight-hour freshness bound for governed-query
// bearer credentials. This is a backend contract (Issue #21): no deployment
// setting may extend it.
const MaxQueryTokenAge = 8 * time.Hour

// QueryExecutionAuthConfig carries the clock injection for query execution
// freshness checks. Clock is injected for tests and defaults to time.Now
// when nil.
type QueryExecutionAuthConfig struct {
	Clock func() time.Time
}

// controlledUnauthorizedMessage is the single client-visible text for every
// unauthenticated outcome (missing, malformed, expired, revoked, disabled,
// Authorization Version mismatch). It must not disclose which check failed.
const controlledUnauthorizedMessage = "unauthorized"

// requireAuthenticatedActor is the chi middleware factory for the base actor
// check: verify token structure/signature and current Authorization Version,
// enforce the fixed MaxQueryTokenAge freshness bound, and store the actor
// user id and current role in context.
func requireAuthenticatedActor(authService *service.AuthService, cfg QueryExecutionAuthConfig, emitter service.AuthAuditEmitter) func(http.Handler) http.Handler {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := verifyBearer(authService, emitter, w, r)
			if !ok {
				return
			}
			// Fixed eight-hour freshness bound (Issue #21). Applies to all
			// protected routes, including ordinary reads.
			if clock().Sub(user.IssuedAt) >= MaxQueryTokenAge {
				emitter.EmitAuthAudit("auth.bearer", "rejected", &user.ID, nil)
				writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
				return
			}
			ctx := context.WithValue(r.Context(), actorContextKey{}, user.ID)
			ctx = context.WithValue(ctx, actorRoleContextKey{}, user.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireAdminActor is the chi middleware factory for admin-only routes.
// It emits auth.authorization denied with the verified actor. The target
// resource id is populated ONLY for routes matching /resources/{id} (and
// sub-paths like /resources/{id}/profile). All other routes — including
// /query-targets/{id} — pass nil to avoid storing non-resource IDs in the
// audit_events.target_resource_id column.
func requireAdminActor(emitter service.AuthAuditEmitter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := actorRoleFromContext(r.Context())
			if role != "admin" {
				var actorID *uint64
				if id, ok := actorUserIDFromContext(r.Context()); ok {
					actorID = &id
				}
				var targetID *uint64
				if id, ok := extractResourceIDFromPath(r.URL.Path); ok {
					targetID = &id
				}
				emitter.EmitAuthAudit("auth.authorization", "denied", actorID, targetID)
				writeJSONError(w, http.StatusForbidden, "forbidden", "admin role is required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractResourceIDFromPath parses a positive integer after /resources/ in the
// URL path. Returns (id, true) only for paths like /resources/{id} or
// /resources/{id}/... . Returns (0, false) for all other paths including
// /query-targets/{id}, /resource-relations/{id}, or malformed paths.
func extractResourceIDFromPath(path string) (uint64, bool) {
	const prefix = "/resources/"
	if !strings.HasPrefix(path, prefix) {
		return 0, false
	}
	rest := path[len(prefix):]
	end := strings.Index(rest, "/")
	if end < 0 {
		end = len(rest)
	}
	if end == 0 {
		return 0, false
	}
	parsed, err := strconv.ParseUint(rest[:end], 10, 64)
	if err != nil || parsed == 0 {
		return 0, false
	}
	return parsed, true
}

// requireFreshQueryActor is the chi middleware factory mounted on query
// execute/history routes. It performs the base current-state check and
// additionally rejects credentials whose embedded issuedAt is at least
// MaxQueryTokenAge old (fixed eight-hour freshness for governed query).
// Expiry uses the same generic 401 as other auth failures.
func requireFreshQueryActor(authService *service.AuthService, cfg QueryExecutionAuthConfig, emitter service.AuthAuditEmitter) func(http.Handler) http.Handler {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := verifyBearer(authService, emitter, w, r)
			if !ok {
				return
			}
			// Query execution is a higher-risk surface than read/list routes,
			// so token freshness is bounded here by a fixed eight-hour constant.
			if clock().Sub(user.IssuedAt) >= MaxQueryTokenAge {
				emitter.EmitAuthAudit("auth.bearer", "rejected", &user.ID, nil)
				writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
				return
			}
			ctx := context.WithValue(r.Context(), actorContextKey{}, user.ID)
			ctx = context.WithValue(ctx, actorRoleContextKey{}, user.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// verifyBearer extracts the bearer token from the Authorization header, verifies
// it via the auth service (signature + current Authorization Version + active
// state), and writes a generic 401 on any failure. On success it returns the
// authenticated user with the current server-owned role.
func verifyBearer(authService *service.AuthService, emitter service.AuthAuditEmitter, w http.ResponseWriter, r *http.Request) (*service.AuthenticatedUser, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, prefix+"chmp_") {
		writeMachineCredentialError(w, service.ErrMachineScopeDenied)
		return nil, false
	}
	if header == "" {
		// Missing Authorization is absence of a credential, not a rejected
		// supplied credential (ADR 2026-08-15): the same controlled 401 with
		// no auth.bearer rejected audit event.
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
		return nil, false
	}
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		emitter.EmitAuthAudit("auth.bearer", "rejected", nil, nil)
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
		return nil, false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		emitter.EmitAuthAudit("auth.bearer", "rejected", nil, nil)
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
		return nil, false
	}
	user, err := authService.VerifyToken(token)
	if err != nil {
		emitter.EmitAuthAudit("auth.bearer", "rejected", nil, nil)
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
		return nil, false
	}
	return user, true
}

// actorUserIDFromContext returns the authenticated actor user id stored by the
// auth middleware, if present. Handlers use this instead of trusting any actor
// id supplied in request JSON or query parameters.
func actorUserIDFromContext(ctx context.Context) (uint64, bool) {
	id, ok := ctx.Value(actorContextKey{}).(uint64)
	return id, ok
}

// actorRoleFromContext returns the authenticated actor's current role stored by
// the auth middleware, if present. The role is taken from verified current
// authorization state, never from the request body.
func actorRoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(actorRoleContextKey{}).(string)
	return role, ok
}
