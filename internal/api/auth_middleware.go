// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, net/http, strings, time, internal/service
// output: QueryExecutionAuthConfig, requireAuthenticatedActor, requireFreshQueryActor, actorUserIDFromContext, actorRoleFromContext
// pos: Bearer auth middleware for query execution routes — structure/signature check plus bounded TTL freshness
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
// actor's role. Phase 38A credential write handlers read it to enforce the
// admin-only boundary; it is stored alongside the user id by the auth middleware.
type actorRoleContextKey struct{}

// QueryExecutionAuthConfig carries the freshness policy for query execution
// routes. TokenMaxAge is the bounded TTL; zero means "reject everything"
// (fail closed), never "allow everything". Clock is injected for tests and
// defaults to time.Now when nil.
type QueryExecutionAuthConfig struct {
	TokenMaxAge time.Duration
	Clock       func() time.Time
}

// requireAuthenticatedActor is the chi middleware factory for the base actor
// check: verify token structure/signature and store the actor user id in
// context. It does NOT enforce TTL and is not mounted directly on query routes;
// query execute/history routes mount requireFreshQueryActor instead. It returns
// the func(http.Handler) http.Handler shape chi's r.Use expects (same shape as
// the corsLocalDev middleware in internal/api/router.go).
func requireAuthenticatedActor(authService *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := verifyBearer(authService, w, r)
			if !ok {
				return
			}
			ctx := context.WithValue(r.Context(), actorContextKey{}, user.ID)
			ctx = context.WithValue(ctx, actorRoleContextKey{}, user.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireFreshQueryActor is the chi middleware factory mounted on query
// execute/history routes. It performs the base check and additionally rejects
// tokens whose embedded issuedAt is older than cfg.TokenMaxAge (computed against
// cfg.Clock and the token's issuedAt). It returns the
// func(http.Handler) http.Handler shape, so it is used as
// r.Use(requireFreshQueryActor(deps.AuthService, cfg)).
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
			// so token freshness is bounded here. A zero/unset TokenMaxAge fails
			// closed (reject) rather than silently allowing every token.
			if cfg.TokenMaxAge <= 0 {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized", "token freshness policy not configured")
				return
			}
			if clock().Sub(user.IssuedAt) > cfg.TokenMaxAge {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized", "token expired")
				return
			}
			ctx := context.WithValue(r.Context(), actorContextKey{}, user.ID)
			ctx = context.WithValue(ctx, actorRoleContextKey{}, user.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// verifyBearer extracts the bearer token from the Authorization header, verifies
// it via the auth service, and writes a 401 on any failure. On success it
// returns the authenticated user. The 401 path never distinguishes missing vs
// invalid tokens to avoid leaking verification internals.
func verifyBearer(authService *service.AuthService, w http.ResponseWriter, r *http.Request) (*service.AuthenticatedUser, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return nil, false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return nil, false
	}
	user, err := authService.VerifyToken(token)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
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

// actorRoleFromContext returns the authenticated actor's role stored by the auth
// middleware, if present. Phase 38A credential write handlers use this to
// enforce the admin-only boundary; the role is taken from the verified token,
// never from the request body.
func actorRoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(actorRoleContextKey{}).(string)
	return role, ok
}
