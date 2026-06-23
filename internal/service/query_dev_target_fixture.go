// Package service provides the local/dev query target fixture.
//
// input: context, errors, net, strconv, strings, go-sql-driver/mysql, internal/model
// output: QueryDevTargetFixtureConfig, DevTargetDictionary, DevTargetResourceStore, QueryDevTargetFixture, NewQueryDevTargetFixture, EnsureLocalQueryTarget, ParseControlHubDSNHostPort, fixture sentinel errors
// pos: Explicit, dev/test-only orchestration that ensures one local database_instance query target + profile so the existing credential seed can make it ready. Reuses existing repository methods only — no new repository method, no migration, no credential-binding change. The DSN never enters this path; host/port arrive pre-parsed.
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
)

// Sentinel errors for the dev target fixture. Fixed strings only — none carries
// the DSN, password, or any env value.
var (
	errFixtureMissingEnvSlug      = errors.New("dev fixture requires an environment slug")
	errFixtureMissingOwnerEmail   = errors.New("dev fixture requires an owner email")
	errFixtureMissingResourceName = errors.New("dev fixture requires a resource name")
	errFixtureInvalidHostPort     = errors.New("dev fixture requires a non-empty host and a positive port")
	errFixtureUnsupportedEngine   = errors.New("dev fixture engine is not executable (mysql/tidb only)")
	errFixtureEnvSlugNotFound     = errors.New("dev fixture environment slug not found")
	errFixtureOwnerEmailNotFound  = errors.New("dev fixture owner email not found")
	errFixtureEnsureFailed        = errors.New("dev fixture ensure failed")
	errFixtureDSNUnparseable      = errors.New("controlhub dsn is unparseable")
	errFixtureDSNNotTCP           = errors.New("controlhub dsn net is not tcp")
	errFixtureDSNPortMissing      = errors.New("controlhub dsn address is missing an explicit port")
	errFixtureDSNAddressMalformed = errors.New("controlhub dsn address is malformed")
)

// devFixtureSource is the Source value the fixture stamps on every resource it
// creates, and the only Source it will reuse. It keeps the fixture from touching
// real (e.g. manual) same-name resources and matches the rollback filter.
const devFixtureSource = "dev-fixture"

// errFixtureExistingResourceNotFixture is returned when a same-name resource
// exists in the target environment but is NOT a dev fixture. The fixture must
// never reuse or overwrite a real resource's profile. Fixed string — no DSN.
var errFixtureExistingResourceNotFixture = errors.New("dev fixture found an existing same-name resource that is not a dev fixture")

// DevTargetDictionary resolves dev fixture identity (environment by slug, owner
// by email). It is satisfied by *mysql.DictionaryRepository.
type DevTargetDictionary interface {
	ListEnvironments() ([]model.Environment, error)
	ListOwners() ([]model.Owner, error)
}

// DevTargetResourceStore finds/creates the fixture resource and upserts its
// profile. It is satisfied by *mysql.ResourceRepository. No method here is new
// to the repository — the fixture deliberately reuses existing surface so no
// production contract is widened.
type DevTargetResourceStore interface {
	ListResources(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, int, error)
	CreateResource(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error)
	UpsertDatabaseInstanceProfile(ctx context.Context, resourceID uint64, engine, version, host string, port int, role string) error
}

// QueryDevTargetFixtureConfig carries the fixture inputs. Host/Port are
// pre-parsed from DATABASE_DSN by the caller (ParseControlHubDSNHostPort); the
// DSN itself never enters the fixture.
type QueryDevTargetFixtureConfig struct {
	EnvironmentSlug string
	OwnerEmail      string
	ResourceName    string
	DisplayName     string
	Engine          string
	Version         string
	Role            string
	Host            string
	Port            int
}

func (c QueryDevTargetFixtureConfig) validate() error {
	if strings.TrimSpace(c.EnvironmentSlug) == "" {
		return errFixtureMissingEnvSlug
	}
	if strings.TrimSpace(c.OwnerEmail) == "" {
		return errFixtureMissingOwnerEmail
	}
	if strings.TrimSpace(c.ResourceName) == "" {
		return errFixtureMissingResourceName
	}
	if strings.TrimSpace(c.Host) == "" || c.Port <= 0 {
		return errFixtureInvalidHostPort
	}
	if !isExecutableEngine(c.Engine) {
		return errFixtureUnsupportedEngine
	}
	return nil
}

// QueryDevTargetFixture ensures one local database_instance query target + profile
// for dev/test, so the existing credential seed (cmd/querydev) can make it ready.
// It is pure orchestration over existing repository methods.
type QueryDevTargetFixture struct {
	dictionary DevTargetDictionary
	resources  DevTargetResourceStore
}

// NewQueryDevTargetFixture wires the fixture with the dictionary (env/owner
// resolution) and the resource store (find/create + profile upsert).
func NewQueryDevTargetFixture(dictionary DevTargetDictionary, resources DevTargetResourceStore) *QueryDevTargetFixture {
	return &QueryDevTargetFixture{dictionary: dictionary, resources: resources}
}

// EnsureLocalQueryTarget resolves env+owner, finds-or-creates the resource, and
// upserts its profile. It returns the resource id. The DSN never enters this
// method; every rejection happens before any write. On a (name, env) unique
// conflict it re-lists and reuses the winner, so repeated runs are idempotent.
func (f *QueryDevTargetFixture) EnsureLocalQueryTarget(ctx context.Context, cfg QueryDevTargetFixtureConfig) (uint64, error) {
	if err := cfg.validate(); err != nil {
		return 0, err
	}

	envID, ok := f.environmentIDBySlug(cfg.EnvironmentSlug)
	if !ok {
		return 0, errFixtureEnvSlugNotFound
	}
	ownerID, ok := f.ownerIDByEmail(cfg.OwnerEmail)
	if !ok {
		return 0, errFixtureOwnerEmailNotFound
	}

	resourceID, err := f.findOrCreate(ctx, cfg, envID, ownerID)
	if err != nil {
		return 0, err
	}

	if err := f.resources.UpsertDatabaseInstanceProfile(ctx, resourceID, cfg.Engine, cfg.Version, cfg.Host, cfg.Port, cfg.Role); err != nil {
		return 0, errFixtureEnsureFailed
	}
	return resourceID, nil
}

func (f *QueryDevTargetFixture) environmentIDBySlug(slug string) (uint64, bool) {
	envs, err := f.dictionary.ListEnvironments()
	if err != nil {
		return 0, false
	}
	for _, e := range envs {
		if e.Slug == slug {
			return e.ID, true
		}
	}
	return 0, false
}

func (f *QueryDevTargetFixture) ownerIDByEmail(email string) (uint64, bool) {
	owners, err := f.dictionary.ListOwners()
	if err != nil {
		return 0, false
	}
	for _, o := range owners {
		if o.Email == email {
			return o.ID, true
		}
	}
	return 0, false
}

func (f *QueryDevTargetFixture) findOrCreate(ctx context.Context, cfg QueryDevTargetFixtureConfig, envID, ownerID uint64) (uint64, error) {
	id, found, err := f.findExisting(ctx, cfg, envID)
	if err != nil {
		// Same-name resource exists but is not a dev fixture: never reuse/overwrite.
		return 0, err
	}
	if found {
		return id, nil
	}
	res, err := f.resources.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: cfg.Engine,
		Name:            cfg.ResourceName,
		DisplayName:     cfg.DisplayName,
		EnvironmentID:   envID,
		OwnerID:         ownerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          devFixtureSource,
		Labels:          map[string]string{},
	})
	if err != nil {
		// A concurrent ensure may have won the (name, env) unique race; re-list. A
		// same-name non-fixture resource causing the conflict must also fail closed
		// (never reused/overwritten). Any other create error fails closed too.
		if errors.Is(err, ErrResourceConflict) {
			id, found, ferr := f.findExisting(ctx, cfg, envID)
			if ferr != nil {
				return 0, ferr
			}
			if found {
				return id, nil
			}
		}
		return 0, errFixtureEnsureFailed
	}
	return res.ID, nil
}

// findExisting locates a same-name database_instance in the resolved environment
// and reports whether it is safe for the fixture to reuse. Returns:
//   - (id, true, nil) when an exact match exists AND is a dev fixture (reuse);
//   - (0, false, nil) when no exact match exists (caller may create);
//   - (0, false, errFixtureExistingResourceNotFixture) when an exact match exists
//     but is NOT a dev fixture — the fixture must never reuse/overwrite a real
//     (e.g. manual) resource's profile.
//
// ListResources' Query is a LIKE search over name/display_name/external_id, so the
// exact match is enforced here. A lookup error is treated as not-found so a
// transient read failure cannot block a fresh create; the unique (name, env) index
// still prevents a duplicate, and the post-conflict re-list re-checks the source.
func (f *QueryDevTargetFixture) findExisting(ctx context.Context, cfg QueryDevTargetFixtureConfig, envID uint64) (uint64, bool, error) {
	items, _, err := f.resources.ListResources(ctx, model.ResourceListQuery{
		ResourceTypes:  []string{string(model.ResourceTypeDatabaseInstance)},
		EnvironmentIDs: []uint64{envID},
		Query:          cfg.ResourceName,
		Page:           1,
		PageSize:       200,
	})
	if err != nil {
		return 0, false, nil
	}
	for _, r := range items {
		if r.Name == cfg.ResourceName && r.ResourceType == model.ResourceTypeDatabaseInstance && r.EnvironmentID == envID {
			if r.Source != devFixtureSource {
				return 0, false, errFixtureExistingResourceNotFixture
			}
			return r.ID, true, nil
		}
	}
	return 0, false, nil
}

// ParseControlHubDSNHostPort parses a go-sql-driver MySQL DSN and returns only
// its host and port. It mirrors validateDSNBinding's portless-DSN defense (the
// driver normalizes a portless tcp address to :3306 during ParseDSN), so a DSN
// without an explicit port is rejected. It never returns or logs the full DSN;
// every error is a fixed string that carries no DSN fragment.
func ParseControlHubDSNHostPort(dsn string) (string, int, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", 0, errFixtureDSNUnparseable
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Net), "tcp") {
		return "", 0, errFixtureDSNNotTCP
	}
	rawAddr, ok := rawAddressFor(dsn, cfg.Net)
	if !ok {
		return "", 0, errFixtureDSNAddressMalformed
	}
	if _, portStr, splitErr := net.SplitHostPort(rawAddr); splitErr != nil || portStr == "" {
		return "", 0, errFixtureDSNPortMissing
	}
	host, portStr, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return "", 0, errFixtureDSNAddressMalformed
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, errFixtureDSNAddressMalformed
	}
	return host, port, nil
}
