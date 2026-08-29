// Package model provides domain entities for the resource management system.
// input: fmt and time
// output: machine-principal records, safe credential lifecycle list metadata, closed scopes, requests, and bounded credential expiry
// pos: Domain contract and pure rules for machine-principal credentials
// note: if this file changes, update this header and module README.md.
package model

import (
	"fmt"
	"time"
)

type MachineScope string

const (
	MachineScopeInventoryRead  MachineScope = "inventory:read"
	MachineScopeRelationsRead  MachineScope = "relations:read"
	MachineScopeGovernedSelect MachineScope = "governed-select"
	MachineScopeAuditRead      MachineScope = "audit:read"
	MachineScopeNamedViewsRead MachineScope = "named-views:read"

	DefaultMachineCredentialLifetime = 30 * 24 * time.Hour
	MaxMachineCredentialLifetime     = 90 * 24 * time.Hour
)

var machineScopes = []MachineScope{
	MachineScopeInventoryRead,
	MachineScopeRelationsRead,
	MachineScopeGovernedSelect,
	MachineScopeAuditRead,
	MachineScopeNamedViewsRead,
}

type MachinePrincipal struct {
	ID              uint64    `json:"id"`
	Name            string    `json:"name"`
	CreatedByUserID uint64    `json:"createdByUserId"`
	CreatedAt       time.Time `json:"createdAt"`
}

// MachinePrincipalListItem is the administrator list projection. It exposes
// only the credential lifecycle fields needed to rotate or revoke after reload.
type MachinePrincipalListItem struct {
	ID              uint64                       `json:"id"`
	Name            string                       `json:"name"`
	CreatedByUserID uint64                       `json:"createdByUserId"`
	CreatedAt       time.Time                    `json:"createdAt"`
	Credentials     []MachineCredentialLifecycle `json:"credentials"`
}

type MachineCredentialLifecycle struct {
	ID         uint64     `json:"id"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

type MachineCredential struct {
	ID                      uint64         `json:"id"`
	MachinePrincipalID      uint64         `json:"machinePrincipalId"`
	LookupID                string         `json:"-"`
	Scopes                  []MachineScope `json:"scopes"`
	ExpiresAt               time.Time      `json:"expiresAt"`
	LastUsedAt              *time.Time     `json:"lastUsedAt"`
	RevokedAt               *time.Time     `json:"revokedAt"`
	RotatedFromCredentialID *uint64        `json:"rotatedFromCredentialId"`
	CreatedAt               time.Time      `json:"createdAt"`
}

type MachinePrincipalCreateRequest struct {
	Name      string         `json:"name"`
	Scopes    []MachineScope `json:"scopes"`
	ExpiresAt *time.Time     `json:"expiresAt,omitempty"`
}

type MachineCredentialRotateRequest struct {
	Scopes    []MachineScope `json:"scopes"`
	ExpiresAt *time.Time     `json:"expiresAt,omitempty"`
}

type MachineCredentialIssue struct {
	Principal  MachinePrincipal  `json:"principal"`
	Credential MachineCredential `json:"credential"`
	Secret     string            `json:"secret"`
}

type MachinePrincipalIdentity struct {
	ID           uint64         `json:"id"`
	Name         string         `json:"name"`
	CredentialID uint64         `json:"credentialId"`
	Scopes       []MachineScope `json:"scopes"`
}

func NormalizeMachineScopes(scopes []MachineScope) ([]MachineScope, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("at least one scope is required")
	}
	requested := make(map[MachineScope]struct{}, len(scopes))
	for _, scope := range scopes {
		if _, exists := requested[scope]; exists {
			return nil, fmt.Errorf("duplicate scope %q", scope)
		}
		requested[scope] = struct{}{}
	}
	normalized := make([]MachineScope, 0, len(scopes))
	for _, scope := range machineScopes {
		if _, exists := requested[scope]; exists {
			normalized = append(normalized, scope)
			delete(requested, scope)
		}
	}
	if len(requested) != 0 {
		return nil, fmt.Errorf("unknown machine scope")
	}
	return normalized, nil
}

func ResolveMachineCredentialExpiry(now time.Time, requested *time.Time) (time.Time, error) {
	now = now.UTC()
	if requested == nil {
		return now.Add(DefaultMachineCredentialLifetime), nil
	}
	expiresAt := requested.UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(MaxMachineCredentialLifetime)) {
		return time.Time{}, fmt.Errorf("expiry must be after now and within 90 days")
	}
	return expiresAt, nil
}
