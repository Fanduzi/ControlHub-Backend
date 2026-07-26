// Package service provides the env-backed credential resolver for the query sandbox.
// input: fmt, os, internal/model
// output: EnvCredentialResolver, NewEnvCredentialResolver (implements QueryCredentialResolver)
// pos: Resolves a validated credential_ref to a DSN from the process environment (DSN never stored in ControlHub tables)
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"fmt"
	"os"

	"github.com/fan/controlhub/internal/model"
)

// credentialEnvPrefix is the environment-variable prefix the resolver reads.
// A credential_ref ORDER_MYSQL_RO resolves from
// CONTROLHUB_QUERY_CREDENTIAL_ORDER_MYSQL_RO.
const credentialEnvPrefix = "CONTROLHUB_QUERY_CREDENTIAL_"

// EnvCredentialResolver resolves a credential_ref to a DSN held in the process
// environment. The DSN/password is never persisted in ControlHub tables, never
// logged, and never returned through any API.
type EnvCredentialResolver struct {
	lookup func(key string) string
}

// NewEnvCredentialResolver builds a resolver that reads os.Getenv.
func NewEnvCredentialResolver() *EnvCredentialResolver {
	return &EnvCredentialResolver{lookup: os.Getenv}
}

// Resolve validates the ref first (fail closed — never performs an env lookup
// with an unvalidated key) and then reads the DSN from the environment. A
// missing or unset credential fails closed; the DSN never appears in the error.
func (r *EnvCredentialResolver) Resolve(_ context.Context, credentialRef string) (string, error) {
	if err := model.ValidateCredentialRef(credentialRef); err != nil {
		return "", err
	}
	dsn := r.lookup(credentialEnvPrefix + credentialRef)
	if dsn == "" {
		return "", fmt.Errorf("credential %s is not configured", credentialRef)
	}
	return dsn, nil
}
