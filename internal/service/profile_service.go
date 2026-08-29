// Package service provides business logic for typed profile writes.
// input: encoding/json, internal/model (ResourceType, Resource), ProfileRepository interface
// output: NewProfileService, ProfileService.PutProfile/PatchProfile/DeleteProfile, ProfileRepository interface, minimum manual identity validation
// pos: Business logic for resource profile upserts with archived-resource guard, strict field validation, Domain Name/Virtual IP identity, Database Proxy/Control Plane identity, PATCH partial-merge semantics, and manual-registration identity
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/model"
)

const manualIdentityRequired = "is required for manual registration"

// ProfileRepository defines the data access interface for typed profile upserts and deletes.
// The concrete MySQL implementation lives in internal/repository/mysql/resource_repository.go.
type ProfileRepository interface {
	UpsertHostProfile(ctx context.Context, resourceID uint64, hostname, ipAddress, osName string) error
	UpsertDatabaseInstanceProfile(ctx context.Context, resourceID uint64, engine, version, host string, port int, role string) error
	UpsertDatabaseClusterProfile(ctx context.Context, resourceID uint64, engine, topologyMode, primaryEndpoint string) error
	UpsertServiceProfile(ctx context.Context, resourceID uint64, systemName, repositoryUrl, runtimeEnv string) error
	UpsertDomainNameProfile(ctx context.Context, resourceID uint64, fqdn string) error
	UpsertVirtualIPProfile(ctx context.Context, resourceID uint64, ipAddress string) error
	UpsertDatabaseProxyProfile(ctx context.Context, resourceID uint64, technologySubtype, host string, port int, role, version string) error
	UpsertControlPlaneComponentProfile(ctx context.Context, resourceID uint64, componentSubtype, endpoint, version, role string) error
	// PatchProfile partially updates a typed profile in one atomic statement:
	// submitted fields are set (explicit empty/zero values honored), omitted
	// fields keep their current values, and the row is created when absent.
	PatchProfile(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]interface{}) error
	DeleteProfile(ctx context.Context, resourceID uint64, resourceType string) error
}

type inventoryAuditProfileRepository interface {
	PutProfileWithAudit(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]any, actorUserID uint64, eventType string) error
	PatchProfileWithAudit(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]any, actorUserID uint64, eventType string) error
	DeleteProfileWithAudit(ctx context.Context, resourceID uint64, resourceType model.ResourceType, actorUserID uint64, eventType string) error
}

// ProfileService handles profile write operations for resources.
type ProfileService struct {
	profileRepo  ProfileRepository
	resourceRepo ResourceRepository
}

// NewProfileService creates a ProfileService with separate profile and resource repositories.
func NewProfileService(profileRepo ProfileRepository, resourceRepo ResourceRepository) *ProfileService {
	return &ProfileService{profileRepo: profileRepo, resourceRepo: resourceRepo}
}

// PutProfile replaces (upserts) the full profile for a resource.
// Returns ErrResourceNotFound if the resource does not exist,
// ErrResourceArchived if the resource is archived, a *ValidationError for
// unknown/malformed/overlong fields, or ErrProfileNotSupported for resource
// types without a profile table.
func (s *ProfileService) PutProfile(ctx context.Context, resourceID uint64, fields map[string]interface{}) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return ErrResourceArchived
	}
	if err := validateProfileFields(res.ResourceType, fields, true); err != nil {
		return err
	}
	return s.writeProfile(ctx, resourceID, res.ResourceType, fields)
}

func (s *ProfileService) PutProfileInventory(ctx context.Context, actorUserID, resourceID uint64, fields map[string]interface{}) error {
	res, err := s.inventoryProfileTarget(resourceID, fields, true)
	if err != nil {
		return err
	}
	repo, ok := s.profileRepo.(inventoryAuditProfileRepository)
	if !ok {
		return fmt.Errorf("inventory audit profile repository is required")
	}
	return repo.PutProfileWithAudit(ctx, resourceID, res.ResourceType, fields, actorUserID, "inventory.profile.updated")
}

// PatchProfile partially updates the profile for a resource: only the
// submitted fields are changed; omitted fields are preserved (explicit empty
// and zero values are honored). An empty body is a no-op. The merge happens
// in a single repository statement so concurrent partial updates cannot
// overwrite each other's fields.
func (s *ProfileService) PatchProfile(ctx context.Context, resourceID uint64, fields map[string]interface{}) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return ErrResourceArchived
	}
	if err := validateProfileFields(res.ResourceType, fields, false); err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}
	return s.profileRepo.PatchProfile(ctx, resourceID, res.ResourceType, fields)
}

func (s *ProfileService) PatchProfileInventory(ctx context.Context, actorUserID, resourceID uint64, fields map[string]interface{}) error {
	res, err := s.inventoryProfileTarget(resourceID, fields, false)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}
	repo, ok := s.profileRepo.(inventoryAuditProfileRepository)
	if !ok {
		return fmt.Errorf("inventory audit profile repository is required")
	}
	return repo.PatchProfileWithAudit(ctx, resourceID, res.ResourceType, fields, actorUserID, "inventory.profile.updated")
}

// DeleteProfile removes the profile row for a resource.
// Returns ErrResourceNotFound if the resource does not exist,
// or ErrResourceArchived if the resource is archived.
func (s *ProfileService) DeleteProfile(ctx context.Context, resourceID uint64) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return ErrResourceArchived
	}
	return s.profileRepo.DeleteProfile(ctx, resourceID, string(res.ResourceType))
}

func (s *ProfileService) DeleteProfileInventory(ctx context.Context, actorUserID, resourceID uint64) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return ErrResourceArchived
	}
	repo, ok := s.profileRepo.(inventoryAuditProfileRepository)
	if !ok {
		return fmt.Errorf("inventory audit profile repository is required")
	}
	return repo.DeleteProfileWithAudit(ctx, resourceID, res.ResourceType, actorUserID, "inventory.profile.deleted")
}

func (s *ProfileService) inventoryProfileTarget(resourceID uint64, fields map[string]interface{}, full bool) (*model.Resource, error) {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return nil, err
	}
	if res.ArchivedAt != nil {
		return nil, ErrResourceArchived
	}
	if err := validateProfileFields(res.ResourceType, fields, full); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *ProfileService) writeProfile(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]interface{}) error {
	switch resourceType {
	case model.ResourceTypeHost:
		return s.profileRepo.UpsertHostProfile(ctx, resourceID,
			getStringField(fields, "hostname"),
			getStringField(fields, "ipAddress"),
			getStringField(fields, "osName"),
		)
	case model.ResourceTypeDatabaseInstance:
		return s.profileRepo.UpsertDatabaseInstanceProfile(ctx, resourceID,
			getStringField(fields, "engine"),
			getStringField(fields, "version"),
			getStringField(fields, "host"),
			getIntField(fields, "port"),
			getStringField(fields, "role"),
		)
	case model.ResourceTypeDatabaseCluster:
		return s.profileRepo.UpsertDatabaseClusterProfile(ctx, resourceID,
			getStringField(fields, "engine"),
			getStringField(fields, "topologyMode"),
			getStringField(fields, "primaryEndpoint"),
		)
	case model.ResourceTypeService:
		return s.profileRepo.UpsertServiceProfile(ctx, resourceID,
			getStringField(fields, "systemName"),
			getStringField(fields, "repositoryUrl"),
			getStringField(fields, "runtimeEnv"),
		)
	case model.ResourceTypeDomainName:
		return s.profileRepo.UpsertDomainNameProfile(ctx, resourceID,
			getStringField(fields, "fqdn"),
		)
	case model.ResourceTypeVirtualIP:
		return s.profileRepo.UpsertVirtualIPProfile(ctx, resourceID,
			getStringField(fields, "ipAddress"),
		)
	case model.ResourceTypeDatabaseProxy:
		return s.profileRepo.UpsertDatabaseProxyProfile(ctx, resourceID,
			getStringField(fields, "technologySubtype"),
			getStringField(fields, "host"),
			getIntField(fields, "port"),
			getStringField(fields, "role"),
			getStringField(fields, "version"),
		)
	case model.ResourceTypeControlPlaneComponent:
		return s.profileRepo.UpsertControlPlaneComponentProfile(ctx, resourceID,
			getStringField(fields, "componentSubtype"),
			getStringField(fields, "endpoint"),
			getStringField(fields, "version"),
			getStringField(fields, "role"),
		)
	default:
		return ErrProfileNotSupported
	}
}

func getStringField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func getIntField(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// profileFieldKind distinguishes string-valued and integer-valued profile fields.
type profileFieldKind int

const (
	profileStringField profileFieldKind = iota
	profileIntField
)

type profileFieldFormat int

const (
	profileFormatNone profileFieldFormat = iota
	profileFormatFQDN
	profileFormatIP
)

// profileFieldSpec describes one accepted profile field: its JSON kind and
// its constraints. maxLen mirrors the MySQL varchar column width in
// characters (utf8mb4); intMin/intMax bound integer fields. identity marks
// the minimum manual-registration fields for the four core CI types.
type profileFieldSpec struct {
	key      string
	kind     profileFieldKind
	maxLen   int
	intMin   int64
	intMax   int64
	required bool
	format   profileFieldFormat
	identity bool
	allowed  []string
}

// profileFieldSchemas is the authoritative per-type profile field contract
// (docs/superpowers/specs/2026-04-22-resource-crud-redesign.md). Keep in sync
// with the resource_profiles_* tables in migrations/0001_initial_schema.sql and 00019.
var profileFieldSchemas = map[model.ResourceType][]profileFieldSpec{
	model.ResourceTypeHost: {
		{key: "hostname", kind: profileStringField, maxLen: 255, identity: true},
		{key: "ipAddress", kind: profileStringField, maxLen: 64, identity: true},
		{key: "osName", kind: profileStringField, maxLen: 255},
	},
	model.ResourceTypeDatabaseInstance: {
		{key: "engine", kind: profileStringField, maxLen: 64, identity: true},
		{key: "version", kind: profileStringField, maxLen: 64},
		{key: "host", kind: profileStringField, maxLen: 255, identity: true},
		{key: "port", kind: profileIntField, intMin: 1, intMax: 65535, identity: true},
		{key: "role", kind: profileStringField, maxLen: 64},
	},
	model.ResourceTypeDatabaseCluster: {
		{key: "engine", kind: profileStringField, maxLen: 64, identity: true},
		{key: "topologyMode", kind: profileStringField, maxLen: 64},
		{key: "primaryEndpoint", kind: profileStringField, maxLen: 255, identity: true},
	},
	model.ResourceTypeService: {
		{key: "systemName", kind: profileStringField, maxLen: 255, identity: true},
		{key: "repositoryUrl", kind: profileStringField, maxLen: 512},
		{key: "runtimeEnv", kind: profileStringField, maxLen: 64},
	},
	model.ResourceTypeDomainName: {
		{key: "fqdn", kind: profileStringField, maxLen: 255, required: true, format: profileFormatFQDN},
	},
	model.ResourceTypeVirtualIP: {
		{key: "ipAddress", kind: profileStringField, maxLen: 64, required: true, format: profileFormatIP},
	},
	model.ResourceTypeDatabaseProxy: {
		{key: "technologySubtype", kind: profileStringField, maxLen: 64, required: true, allowed: []string{"proxysql", "chproxy", "haproxy", "maxscale"}},
		{key: "host", kind: profileStringField, maxLen: 255, required: true},
		{key: "port", kind: profileIntField, intMin: 1, intMax: 65535, required: true},
		{key: "role", kind: profileStringField, maxLen: 64, required: true, allowed: []string{"active", "standby"}},
		{key: "version", kind: profileStringField, maxLen: 64},
	},
	model.ResourceTypeControlPlaneComponent: {
		{key: "componentSubtype", kind: profileStringField, maxLen: 64, required: true, allowed: []string{"orchestrator", "ha_monitor", "backup_manager"}},
		{key: "endpoint", kind: profileStringField, maxLen: 255, required: true},
		{key: "version", kind: profileStringField, maxLen: 64},
		{key: "role", kind: profileStringField, maxLen: 64, required: true, allowed: []string{"active", "standby"}},
	},
}

// validateProfileFields rejects unknown fields, non-string values for string
// fields, fractional or non-integer values for integer fields, out-of-range
// integers, overlong strings, FQDN/IP formats, and values outside allowed enumerations.
// Resource types without a profile table return ErrProfileNotSupported.
// Explicit empty strings are valid unless the field is required identity.
// full requires every required identity field.
func validateProfileFields(resourceType model.ResourceType, fields map[string]interface{}, full bool) error {
	specs, ok := profileFieldSchemas[resourceType]
	if !ok {
		return ErrProfileNotSupported
	}
	specByKey := make(map[string]profileFieldSpec, len(specs))
	for _, spec := range specs {
		specByKey[spec.key] = spec
	}

	var ve *ValidationError
	for key, value := range fields {
		spec, ok := specByKey[key]
		if !ok {
			if ve == nil {
				ve = newValidationError("validation failed")
			}
			ve.WithField(key, "unknown profile field")
			continue
		}
		switch spec.kind {
		case profileStringField:
			s, ok := value.(string)
			if !ok {
				if ve == nil {
					ve = newValidationError("validation failed")
				}
				ve.WithField(key, "must be a string")
				continue
			}
			if utf8.RuneCountInString(s) > spec.maxLen {
				if ve == nil {
					ve = newValidationError("validation failed")
				}
				ve.WithField(key, fmt.Sprintf("must be at most %d characters", spec.maxLen))
			}
			switch spec.format {
			case profileFormatFQDN:
				normalized := normalizeFQDN(s)
				fields[key] = normalized
				if spec.required && normalized == "" {
					if ve == nil {
						ve = newValidationError("validation failed")
					}
					ve.WithField(key, "fqdn is required")
				}
			case profileFormatIP:
				trimmed := strings.TrimSpace(s)
				fields[key] = trimmed
				if spec.required && trimmed == "" {
					if ve == nil {
						ve = newValidationError("validation failed")
					}
					ve.WithField(key, "ipAddress is required")
				} else if trimmed != "" && net.ParseIP(trimmed) == nil {
					if ve == nil {
						ve = newValidationError("validation failed")
					}
					ve.WithField(key, "must be a single IPv4 or IPv6 address")
				}
			}
			if spec.format == profileFormatNone {
				if spec.required && s == "" {
					if ve == nil {
						ve = newValidationError("validation failed")
					}
					ve.WithField(key, spec.key+" is required")
				} else if s != "" && len(spec.allowed) > 0 && !slices.Contains(spec.allowed, s) {
					if ve == nil {
						ve = newValidationError("validation failed")
					}
					ve.WithField(key, "must be one of "+fmt.Sprintf("%v", spec.allowed))
				}
			}
		case profileIntField:
			n, ok := profileIntValue(value)
			if !ok {
				if ve == nil {
					ve = newValidationError("validation failed")
				}
				ve.WithField(key, "must be an integer")
				continue
			}
			if n < spec.intMin || n > spec.intMax {
				if ve == nil {
					ve = newValidationError("validation failed")
				}
				ve.WithField(key, fmt.Sprintf("must be between %d and %d", spec.intMin, spec.intMax))
			}
		}
	}
	if full {
		for _, spec := range specs {
			if !spec.required {
				continue
			}
			if _, ok := fields[spec.key]; ok {
				continue
			}
			if ve == nil {
				ve = newValidationError("validation failed")
			}
			if spec.format == profileFormatFQDN {
				ve.WithField(spec.key, "fqdn is required")
				continue
			}
			if spec.format == profileFormatIP {
				ve.WithField(spec.key, "ipAddress is required")
				continue
			}
			ve.WithField(spec.key, spec.key+" is required")
		}
	}
	if ve != nil {
		return ve
	}
	return nil
}

func normalizeFQDN(value string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(value)), ".")
}

// validateMinimumManualIdentity requires each identity field for host,
// database_instance, database_cluster, and service on manual registration.
// Types without a profile table have no T02 identity rule. Labels never
// satisfy identity.
func validateMinimumManualIdentity(resourceType model.ResourceType, fields map[string]interface{}) error {
	specs, ok := profileFieldSchemas[resourceType]
	if !ok {
		return nil
	}
	var ve *ValidationError
	for _, spec := range specs {
		if !spec.identity {
			continue
		}
		if !identityValuePresent(spec, fields) {
			if ve == nil {
				ve = newValidationError("validation failed")
			}
			ve.WithField(spec.key, manualIdentityRequired)
		}
	}
	if ve != nil {
		return ve
	}
	return nil
}

func identityValuePresent(spec profileFieldSpec, fields map[string]interface{}) bool {
	if fields == nil {
		return false
	}
	value, ok := fields[spec.key]
	if !ok || value == nil {
		return false
	}
	switch spec.kind {
	case profileStringField:
		s, ok := value.(string)
		return ok && strings.TrimSpace(s) != ""
	case profileIntField:
		n, ok := profileIntValue(value)
		return ok && n >= spec.intMin && n <= spec.intMax
	default:
		return false
	}
}

// profileIntValue accepts int, int64, and integral float64 (the shape JSON
// numbers take after decoding). Fractional and non-numeric values are
// rejected.
func profileIntValue(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		parsed, err := n.Int64()
		return parsed, err == nil
	case float64:
		if n != math.Trunc(n) {
			return 0, false
		}
		return int64(n), true
	default:
		return 0, false
	}
}
