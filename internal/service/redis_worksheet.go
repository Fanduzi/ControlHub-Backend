package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fan/controlhub/internal/model"
)

const (
	redisWorksheetTimeout = 2 * time.Second
	redisManifestMaxBytes = 64 << 10
	redisKeyPattern       = `[A-Za-z0-9:_-]{1,128}`
)

var (
	redisKey      = regexp.MustCompile(`^` + redisKeyPattern + `$`)
	redisGet      = regexp.MustCompile(`^(?i:GET)[ \t]+(` + redisKeyPattern + `)$`)
	redisUsername = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// RedisWorksheet binds one trusted non-production Redis target to DB 0.
// Each discovery or Run reads one snapshot of the operator-owned approval.
type RedisWorksheet struct {
	path      string
	execution *QueryExecutionService
}

func NewRedisWorksheet(path string, execution *QueryExecutionService) *RedisWorksheet {
	worksheet := &RedisWorksheet{path: path, execution: execution}
	execution.redisWorksheet = worksheet
	return worksheet
}

type redisApproval struct {
	TargetID      uint64                                `json:"targetId"`
	Environment   string                                `json:"environment"`
	Address       string                                `json:"address"`
	User          string                                `json:"user"`
	CredentialRef string                                `json:"credentialRef"`
	CAPEM         string                                `json:"caPem"`
	Users         []uint64                              `json:"users"`
	Keys          map[string]model.ResultDisclosureMode `json:"keys"`
}

func (s *RedisWorksheet) approval(target model.QueryTarget, user uint64) (redisApproval, error) {
	var a redisApproval
	t := target.ConnectionContext
	if t.Engine != "redis" || !isApprovedNonProduction(t.Environment) || t.Host == "" || t.Port < 1 || t.Port > 65535 || user == 0 {
		return a, ErrQueryNotAllowed
	}
	dir, err := os.Lstat(filepath.Dir(s.path))
	if err != nil || !dir.IsDir() || dir.Mode().Perm()&0022 != 0 {
		return a, ErrQueryNotAllowed
	}
	info, err := os.Lstat(s.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > redisManifestMaxBytes {
		return a, ErrQueryNotAllowed
	}
	f, err := os.Open(s.path)
	if err != nil {
		return a, ErrQueryNotAllowed
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return a, ErrQueryNotAllowed
	}
	data, err := io.ReadAll(io.LimitReader(f, redisManifestMaxBytes+1))
	if err != nil || len(data) > redisManifestMaxBytes || json.Unmarshal(data, &a) != nil {
		return redisApproval{}, ErrQueryNotAllowed
	}
	if a.TargetID != target.ResourceID || a.Environment != t.Environment || a.Address != net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) || !slices.Contains(a.Users, user) ||
		!redisUsername.MatchString(a.User) || a.User == "default" || model.ValidateCredentialRef(a.CredentialRef) != nil || len(a.Keys) == 0 {
		return redisApproval{}, ErrQueryNotAllowed
	}
	for key, mode := range a.Keys {
		if !redisKey.MatchString(key) || mode.Validate() != nil {
			return redisApproval{}, ErrQueryNotAllowed
		}
	}
	return a, nil
}

func redisCredentialURL(secret, address string) (*url.URL, error) {
	u, err := url.Parse(secret)
	if err != nil || u.Scheme != "rediss" || u.Host != address || u.Path != "/0" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.User == nil || !redisUsername.MatchString(u.User.Username()) || u.User.Username() == "default" {
		return nil, ErrQueryNotAllowed
	}
	if password, ok := u.User.Password(); !ok || password == "" {
		return nil, ErrQueryNotAllowed
	}
	return u, nil
}

func (s *RedisWorksheet) binding(ctx context.Context, target model.QueryTarget, a redisApproval) (string, *tls.Config, error) {
	meta, err := s.execution.executions.GetCredential(ctx, target.ResourceID, "")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, model.ErrInvalidCredentialMetadata) {
			return "", nil, ErrQueryNotAllowed
		}
		return "", nil, ErrQueryBackendFailure
	}
	if !meta.Enabled || meta.Engine != "redis" || meta.EnvironmentPolicy != model.QueryEnvPolicyNonProdOnly || meta.CredentialRef != a.CredentialRef || meta.ValidateConnectionIdentity() != nil {
		return "", nil, ErrQueryNotAllowed
	}
	secret, err := s.execution.credentials.Resolve(ctx, a.CredentialRef)
	if err != nil {
		return "", nil, ErrQueryNotAllowed
	}
	u, err := redisCredentialURL(secret, a.Address)
	roots := x509.NewCertPool()
	if err != nil || u.User.Username() != a.User || !roots.AppendCertsFromPEM([]byte(a.CAPEM)) {
		return "", nil, ErrQueryNotAllowed
	}
	password, _ := u.User.Password()
	return password, &tls.Config{RootCAs: roots, ServerName: target.ConnectionContext.Host, MinVersion: tls.VersionTLS12}, nil
}

// ApplyWorksheetActions projects the caller's approved keys into the existing object list.
func (s *RedisWorksheet) ApplyWorksheetActions(ctx context.Context, target *model.QueryTarget, user uint64) {
	a, err := s.approval(*target, user)
	if err != nil {
		return
	}
	if _, _, err := s.binding(ctx, *target, a); err != nil {
		return
	}
	keys := make([]string, 0, len(a.Keys))
	for key := range a.Keys {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		target.SchemaPreview = append(target.SchemaPreview, model.QueryTargetSchemaPreviewNode{Kind: "redis_key", Name: key})
	}
	target.AvailableActions.Run = true
	target.Readiness = model.ReadinessReady
	target.MissingFields = []string{}
	target.Governance.ExecutionEnabled = true
	target.Governance.SafetyState = model.SafetyStateReadonlySandboxEnabled
	target.Governance.CredentialState = string(model.QueryCredentialRuntimeSecretResolved)
	target.Governance.SafetyNote = "Approved read-only Redis worksheet"
	target.Governance.PolicyNotes = []string{"Non-production, GET on approved keys in DB 0"}
}

func (s *RedisWorksheet) execute(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	started := time.Now()
	attempt, cancel := context.WithTimeout(ctx, redisWorksheetTimeout)
	defer cancel()
	rec := model.QueryExecutionRecord{TargetResourceID: target.ResourceID, Engine: "redis", StatementPreview: "Redis GET", Status: model.QueryExecutionSuccess}
	if identity.Kind == model.QueryExecutionActorUser {
		rec.ActorUserID = identity.ID
	} else {
		rec.ActorMachinePrincipalID = identity.ID
	}
	columns, rows, err := s.read(attempt, identity, target, req, &rec)
	if attempt.Err() != nil {
		err = attempt.Err()
	}
	if err == nil {
		rec.RowCount = len(rows)
	} else {
		switch {
		case errors.Is(err, ErrQueryNotAllowed):
			rec.Status, rec.ErrorCode = model.QueryExecutionRejected, "query_not_allowed"
		case errors.Is(err, ErrQueryValidationFailed):
			rec.Status, rec.ErrorCode = model.QueryExecutionRejected, "validation_failed"
		default:
			rec.Status, err, rec.ErrorCode, rec.ErrorMessage = classifyExecutorError(err)
		}
	}
	rec.DurationMs = time.Since(started).Milliseconds()
	id, persistErr := s.execution.persistEvidencePair(ctx, rec, executionEvidenceQuery)
	if persistErr != nil {
		return model.QueryExecuteResponse{}, ErrQueryBackendFailure
	}
	if err != nil {
		return model.QueryExecuteResponse{}, err
	}
	return model.QueryExecuteResponse{
		ExecutionID: id, TargetResourceID: target.ResourceID, Engine: rec.Engine, Status: rec.Status,
		Columns: columns, Rows: rows, RowCount: rec.RowCount, LimitApplied: 1,
		CellTruncated: [][]bool{{false}},
		ExecutedAt:    started.UTC(), DurationMs: rec.DurationMs,
	}, nil
}

func (s *RedisWorksheet) read(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, req model.QueryExecuteRequest, rec *model.QueryExecutionRecord) ([]model.QueryResultColumn, [][]any, error) {
	if identity.Kind != model.QueryExecutionActorUser {
		return nil, nil, ErrQueryNotAllowed
	}
	a, err := s.approval(target, identity.ID)
	if err != nil {
		return nil, nil, err
	}
	if req.Database != "" || req.Schema != "" || req.ClientExecutionID != "" || req.ClickHouseGeneration != "" || req.Pagination != nil || len(req.Statement) > 160 {
		return nil, nil, ErrQueryValidationFailed
	}
	parts := redisGet.FindStringSubmatch(strings.Trim(req.Statement, " \t\r\n"))
	if parts == nil {
		return nil, nil, ErrQueryValidationFailed
	}
	key := parts[1]
	mode, ok := a.Keys[key]
	if !ok {
		return nil, nil, ErrQueryDisclosureBlocked
	}
	password, tlsConfig, err := s.binding(ctx, target, a)
	if err != nil {
		return nil, nil, err
	}
	value, err := readRedisGET(ctx, a.Address, a.User, password, tlsConfig, key, &rec.RemoteState)
	if err != nil {
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return nil, nil, context.DeadlineExceeded
		}
		return nil, nil, err
	}
	plan := DisclosurePlan{Columns: []ColumnDisclosure{{Mode: mode, CopyAllowed: mode == model.ResultDisclosureRawCopyAllowed}}}
	return s.execution.disclosure.Apply(plan, []model.QueryResultColumn{{Name: "value", DatabaseType: "string", Nullable: true}}, [][]any{{value}})
}
