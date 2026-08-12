// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, net/http, strings, time, internal/service
// output: QueryExecutionAuthConfig, requireAuthenticatedActor, requireAdminActor, requireFreshQueryActor, actorUserIDFromContext, actorRoleFromContext
// pos: Bearer auth middleware — signature + current Authorization Version check; admin routes and query routes add role and freshness gates
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"net/http"
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

// QueryExecutionAuthConfig carries the freshness policy for query execution
// routes. TokenMaxAge is the bounded TTL; zero means "reject everything"
// (fail closed), never "allow everything". Clock is injected for tests and
// defaults to time.Now when nil.
type QueryExecutionAuthConfig struct {
	TokenMaxAge time.Duration
	Clock       func() time.Time
}

// controlledUnauthorizedMessage is the single client-visible text for every
// unauthenticated outcome (missing, malformed, expired, revoked, disabled,
// Authorization Version mismatch). It must not disclose which check failed.
const controlledUnauthorizedMessage = "unauthorized"

// requireAuthenticatedActor is the chi middleware factory for the base actor
// check: verify token structure/signature and current Authorization Version,
// then store the actor user id and current role in context. It does NOT enforce
// TTL; query execute/history routes mount requireFreshQueryActor instead.
func requireAuthenticatedActor(authService *service.AuthService, emitter service.AuthAuditEmitter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := verifyBearer(authService, emitter, w, r)
			if !ok {
				return
			}
			ctx := context.WithValue(r.Context(), actorContextKey{}, user.ID)
			ctx = context.WithValue(ctx, actorRoleContextKey{}, user.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requireAdminActor(emitter service.AuthAuditEmitter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := actorRoleFromContext(r.Context())
			if role != "admin" {
				// Actor is authenticated but lacks admin role: emit auth.authorization denied.
				var actorID *uint64
				if id, ok := actorUserIDFromContext(r.Context()); ok {
					actorID = &id
				}
				emitter.EmitAuthAudit("auth.authorization", "denied", actorID, nil)
				writeJSONError(w, http.StatusForbidden, "forbidden", "admin role is required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireFreshQueryActor is the chi middleware factory mounted on query
// execute/history routes. It performs the base current-state check and
// additionally rejects credentials whose embedded issuedAt is older than
// cfg.TokenMaxAge (fixed eight-hour freshness for governed query). Expiry uses
// the same generic 401 as other auth failures.
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
			if cfg.TokenMaxAge <= 0 || clock().Sub(user.IssuedAt) > cfg.TokenMaxAge {
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
