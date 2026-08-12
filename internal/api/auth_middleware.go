// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, net/http, strings, time, internal/service
// output: MaxQueryTokenAge, QueryExecutionAuthConfig, requireAuthenticatedActor, requireAdminActor, requireFreshQueryActor, actorUserIDFromContext, actorRoleFromContext
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
func requireAuthenticatedActor(authService *service.AuthService, cfg QueryExecutionAuthConfig) func(http.Handler) http.Handler {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := verifyBearer(authService, w, r)
			if !ok {
				return
			}
			if clock().Sub(user.IssuedAt) > MaxQueryTokenAge {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
				return
			}
			ctx := context.WithValue(r.Context(), actorContextKey{}, user.ID)
			ctx = context.WithValue(ctx, actorRoleContextKey{}, user.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requireAdminActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := actorRoleFromContext(r.Context())
		if role != "admin" {
			writeJSONError(w, http.StatusForbidden, "forbidden", "admin role is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireFreshQueryActor is the chi middleware factory mounted on query
// execute/history routes. It performs the base current-state check and
// additionally rejects credentials whose embedded issuedAt is older than
// MaxQueryTokenAge (fixed eight-hour freshness for governed query). Expiry
// uses the same generic 401 as other auth failures.
func requireFreshQueryActor(authService *service.AuthService, cfg QueryExecutionAuthConfig) func(http.Handler) http.Handler {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := verifyBearer(authService, w, r)
			if !ok {
				return
			}
			// WHY: query execution is a higher-risk surface than read/list routes,
			// so token freshness is bounded here by a fixed eight-hour constant.
			if clock().Sub(user.IssuedAt) > MaxQueryTokenAge {
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
func verifyBearer(authService *service.AuthService, w http.ResponseWriter, r *http.Request) (*service.AuthenticatedUser, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
		return nil, false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", controlledUnauthorizedMessage)
		return nil, false
	}
	user, err := authService.VerifyToken(token)
	if err != nil {
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
