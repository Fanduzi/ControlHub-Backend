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
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const (
	mongoWorksheetTimeout = 2 * time.Second
	mongoManifestMaxBytes = 64 << 10
	mongoStatementMaxLen  = 4096
	mongoFieldValueLimit  = 1024
	mongoRowLimitDefault  = 50
	mongoRowLimitMax      = 100
	mongoFilterFieldMax   = 16
	mongoCollectionFields = 32
	mongoDefaultMaxTimeMS = 500
	mongoComment          = "controlhub-worksheet"
)

var (
	mongoName  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	mongoField = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	mongoFind  = regexp.MustCompile(`^db\.([A-Za-z0-9_-]{1,64})\.find\(`)
)

// MongoWorksheet binds one trusted non-production MongoDB target to one
// approved database. Each discovery or Run reads one snapshot of the
// operator-owned approval; every Run is an index-gated find.
type MongoWorksheet struct {
	path      string
	execution *QueryExecutionService
	limiter   *worksheetRunLimiter
}

func NewMongoWorksheet(path string, execution *QueryExecutionService) *MongoWorksheet {
	worksheet := &MongoWorksheet{path: path, execution: execution, limiter: newWorksheetRunLimiter(time.Now)}
	execution.mongoWorksheet = worksheet
	return worksheet
}

type mongoApproval struct {
	TargetID      uint64                                           `json:"targetId"`
	Environment   string                                           `json:"environment"`
	Address       string                                           `json:"address"`
	User          string                                           `json:"user"`
	CredentialRef string                                           `json:"credentialRef"`
	CAPEM         string                                           `json:"caPem"`
	Users         []uint64                                         `json:"users"`
	Database      string                                           `json:"database"`
	Collections   map[string]map[string]model.ResultDisclosureMode `json:"collections"`
	MaxTimeMS     *int                                             `json:"maxTimeMS"`
	Limits        *worksheetRunLimitsWire                          `json:"limits"`

	limits    worksheetRunLimits
	maxTimeMS int64
}

func mongoFieldNameValid(name string) bool {
	return name == "_id" || (mongoField.MatchString(name) && !strings.HasPrefix(name, "__"))
}

func (s *MongoWorksheet) approval(target model.QueryTarget, user uint64) (mongoApproval, error) {
	var a mongoApproval
	t := target.ConnectionContext
	if t.Engine != "mongodb" || !isApprovedNonProduction(t.Environment) || t.Host == "" || t.Port < 1 || t.Port > 65535 || user == 0 {
		return a, ErrQueryNotAllowed
	}
	dir, err := os.Lstat(filepath.Dir(s.path))
	if err != nil || !dir.IsDir() || dir.Mode().Perm()&0022 != 0 {
		return a, ErrQueryNotAllowed
	}
	info, err := os.Lstat(s.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > mongoManifestMaxBytes {
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
	data, err := io.ReadAll(io.LimitReader(f, mongoManifestMaxBytes+1))
	if err != nil || len(data) > mongoManifestMaxBytes || json.Unmarshal(data, &a) != nil {
		return mongoApproval{}, ErrQueryNotAllowed
	}
	if a.TargetID != target.ResourceID || a.Environment != t.Environment || a.Address != net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) || !slices.Contains(a.Users, user) ||
		!mongoName.MatchString(a.User) || model.ValidateCredentialRef(a.CredentialRef) != nil || !mongoName.MatchString(a.Database) || len(a.Collections) == 0 {
		return mongoApproval{}, ErrQueryNotAllowed
	}
	for collection, fields := range a.Collections {
		if !mongoName.MatchString(collection) || len(fields) == 0 || len(fields) > mongoCollectionFields {
			return mongoApproval{}, ErrQueryNotAllowed
		}
		for field, mode := range fields {
			if !mongoFieldNameValid(field) || mode.Validate() != nil {
				return mongoApproval{}, ErrQueryNotAllowed
			}
		}
	}
	maxTimeMS := int64(mongoDefaultMaxTimeMS)
	if a.MaxTimeMS != nil {
		if *a.MaxTimeMS < 50 || *a.MaxTimeMS > 2000 {
			return mongoApproval{}, ErrQueryNotAllowed
		}
		maxTimeMS = int64(*a.MaxTimeMS)
	}
	a.maxTimeMS = maxTimeMS
	limits, ok := a.Limits.resolve()
	if !ok {
		return mongoApproval{}, ErrQueryNotAllowed
	}
	a.limits = limits
	return a, nil
}

type mongoCredential struct {
	username   string
	password   string
	authSource string
}

// mongoCredentialURL accepts exactly
// mongodb://<user>:<password>@<host:port>/<database> plus an optional single
// authSource=<db> query parameter. Anything else fails closed. An empty
// database checks shape only — the binding layer always passes the approval
// database so the credential can never target a different one.
func mongoCredentialURL(secret, address, database string) (mongoCredential, error) {
	u, err := url.Parse(secret)
	if err != nil || u.Scheme != "mongodb" || u.Host != address || u.ForceQuery || u.Fragment != "" || u.User == nil {
		return mongoCredential{}, ErrQueryNotAllowed
	}
	pathDB := strings.TrimPrefix(u.Path, "/")
	if !mongoName.MatchString(pathDB) || (database != "" && pathDB != database) || !mongoName.MatchString(u.User.Username()) {
		return mongoCredential{}, ErrQueryNotAllowed
	}
	password, ok := u.User.Password()
	if !ok || password == "" {
		return mongoCredential{}, ErrQueryNotAllowed
	}
	authSource := pathDB
	if u.RawQuery != "" {
		params, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(params) != 1 || len(params["authSource"]) != 1 || !mongoName.MatchString(params["authSource"][0]) {
			return mongoCredential{}, ErrQueryNotAllowed
		}
		authSource = params["authSource"][0]
	}
	return mongoCredential{username: u.User.Username(), password: password, authSource: authSource}, nil
}

func (s *MongoWorksheet) binding(ctx context.Context, target model.QueryTarget, a mongoApproval) (mongoCredential, *tls.Config, error) {
	meta, err := s.execution.executions.GetCredential(ctx, target.ResourceID, "")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, model.ErrInvalidCredentialMetadata) {
			return mongoCredential{}, nil, ErrQueryNotAllowed
		}
		return mongoCredential{}, nil, ErrQueryBackendFailure
	}
	if !meta.Enabled || meta.Engine != "mongodb" || meta.EnvironmentPolicy != model.QueryEnvPolicyNonProdOnly || meta.CredentialRef != a.CredentialRef || meta.ValidateConnectionIdentity() != nil {
		return mongoCredential{}, nil, ErrQueryNotAllowed
	}
	secret, err := s.execution.credentials.Resolve(ctx, a.CredentialRef)
	if err != nil {
		return mongoCredential{}, nil, ErrQueryNotAllowed
	}
	cred, err := mongoCredentialURL(secret, a.Address, a.Database)
	roots := x509.NewCertPool()
	if err != nil || cred.username != a.User || !roots.AppendCertsFromPEM([]byte(a.CAPEM)) {
		return mongoCredential{}, nil, ErrQueryNotAllowed
	}
	return cred, &tls.Config{RootCAs: roots, ServerName: target.ConnectionContext.Host, MinVersion: tls.VersionTLS12}, nil
}

// ApplyWorksheetActions projects the caller's approved collections and fields
// into the existing object list.
func (s *MongoWorksheet) ApplyWorksheetActions(ctx context.Context, target *model.QueryTarget, user uint64) {
	a, err := s.approval(*target, user)
	if err != nil {
		return
	}
	if _, _, err := s.binding(ctx, *target, a); err != nil {
		return
	}
	collections := make([]string, 0, len(a.Collections))
	for collection := range a.Collections {
		collections = append(collections, collection)
	}
	slices.Sort(collections)
	for _, collection := range collections {
		node := model.QueryTargetSchemaPreviewNode{Kind: "mongo_collection", Name: collection}
		fields := make([]string, 0, len(a.Collections[collection]))
		for field := range a.Collections[collection] {
			fields = append(fields, field)
		}
		slices.Sort(fields)
		for _, field := range fields {
			node.Children = append(node.Children, model.QueryTargetSchemaPreviewNode{Kind: "mongo_field", Name: field})
		}
		target.SchemaPreview = append(target.SchemaPreview, node)
	}
	target.AvailableActions.Run = true
	target.Readiness = model.ReadinessReady
	target.MissingFields = []string{}
	target.Governance.ExecutionEnabled = true
	target.Governance.SafetyState = model.SafetyStateReadonlySandboxEnabled
	target.Governance.CredentialState = string(model.QueryCredentialRuntimeSecretResolved)
	target.Governance.SafetyNote = "Approved read-only MongoDB worksheet"
	target.Governance.PolicyNotes = []string{"Non-production, index-backed find on approved collections and fields"}
}

func (s *MongoWorksheet) execute(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	started := time.Now()
	attempt, cancel := context.WithTimeout(ctx, mongoWorksheetTimeout)
	defer cancel()
	rec := model.QueryExecutionRecord{TargetResourceID: target.ResourceID, Engine: "mongodb", StatementPreview: "MongoDB find", Status: model.QueryExecutionSuccess}
	if identity.Kind == model.QueryExecutionActorUser {
		rec.ActorUserID = identity.ID
	} else {
		rec.ActorMachinePrincipalID = identity.ID
	}
	columns, rows, truncated, cellTruncated, limitApplied, err := s.read(attempt, identity, target, req, &rec)
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
		Columns: columns, Rows: rows, RowCount: rec.RowCount, Truncated: truncated, LimitApplied: limitApplied,
		CellTruncated: cellTruncated,
		ExecutedAt:    started.UTC(), DurationMs: rec.DurationMs,
	}, nil
}

func (s *MongoWorksheet) read(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, req model.QueryExecuteRequest, rec *model.QueryExecutionRecord) ([]model.QueryResultColumn, [][]any, bool, [][]bool, int, error) {
	if identity.Kind != model.QueryExecutionActorUser {
		return nil, nil, false, nil, 0, ErrQueryNotAllowed
	}
	a, err := s.approval(target, identity.ID)
	if err != nil {
		return nil, nil, false, nil, 0, err
	}
	if (req.Database != "" && req.Database != a.Database) || req.Schema != "" || req.ClientExecutionID != "" || req.ClickHouseGeneration != "" || req.Pagination != nil || len(req.Statement) > mongoStatementMaxLen {
		return nil, nil, false, nil, 0, ErrQueryValidationFailed
	}
	query, err := parseMongoFind(strings.Trim(req.Statement, " \t\r\n"), a)
	if err != nil {
		return nil, nil, false, nil, 0, err
	}
	maxRows := req.MaxRows
	if maxRows <= 0 {
		maxRows = mongoRowLimitDefault
	}
	if maxRows > mongoRowLimitMax {
		maxRows = mongoRowLimitMax
	}
	cred, tlsConfig, err := s.binding(ctx, target, a)
	if err != nil {
		return nil, nil, false, nil, 0, err
	}
	release, ok := s.limiter.acquire(target.ResourceID, identity.ID, a.limits)
	if !ok {
		return nil, nil, false, nil, 0, ErrQueryRateLimited
	}
	defer release()
	fields, truncated, err := s.runFind(ctx, a, cred, tlsConfig, query, maxRows, &rec.RemoteState)
	if err != nil {
		return nil, nil, false, nil, 0, err
	}
	columns := make([]model.QueryResultColumn, 0, len(query.columns))
	plan := DisclosurePlan{Columns: make([]ColumnDisclosure, 0, len(query.columns))}
	for _, name := range query.columns {
		mode := a.Collections[query.collection][name]
		columns = append(columns, model.QueryResultColumn{Name: name, DatabaseType: "bson", Nullable: true})
		plan.Columns = append(plan.Columns, ColumnDisclosure{Mode: mode, CopyAllowed: mode == model.ResultDisclosureRawCopyAllowed})
	}
	rows := make([][]any, 0, len(fields))
	cellTruncated := make([][]bool, 0, len(fields))
	for _, doc := range fields {
		row := make([]any, len(query.columns))
		marks := make([]bool, len(query.columns))
		for i, name := range query.columns {
			cell := doc[name]
			if cell.truncated {
				marks[i] = true
			} else {
				row[i] = cell.value
			}
		}
		rows = append(rows, row)
		cellTruncated = append(cellTruncated, marks)
	}
	columns, rows, err = s.execution.disclosure.Apply(plan, columns, rows)
	return columns, rows, truncated, cellTruncated, maxRows, err
}

// runFind connects, proves the plan is index-backed, then runs the capped find.
// The connection lives for exactly one attempt: no pooling, no reuse.
func (s *MongoWorksheet) runFind(ctx context.Context, a mongoApproval, cred mongoCredential, tlsConfig *tls.Config, query mongoFindQuery, maxRows int, remote *model.QueryExecutionRemoteState) ([]map[string]mongoCell, bool, error) {
	opts := options.Client().
		SetHosts([]string{a.Address}).
		SetDirect(true).
		SetAuth(options.Credential{AuthMechanism: "SCRAM-SHA-256", Username: cred.username, Password: cred.password, AuthSource: cred.authSource}).
		SetTLSConfig(tlsConfig).
		SetReadPreference(readpref.SecondaryPreferred()).
		SetMaxPoolSize(1).
		SetRetryReads(false).
		SetRetryWrites(false).
		SetAppName(mongoComment)
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, false, context.DeadlineExceeded
		}
		opts.SetServerSelectionTimeout(remaining).SetConnectTimeout(remaining)
	}
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, false, ErrQueryBackendFailure
	}
	defer func() {
		// A wedged server must not hold the goroutine or the limiter slots.
		disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), time.Second)
		defer disconnectCancel()
		_ = client.Disconnect(disconnectCtx)
	}()
	db := client.Database(a.Database)

	// The explain is the first server command; past this point a disconnect
	// can no longer prove the server saw nothing.
	*remote = model.QueryExecutionRemoteUnknown
	explain := bson.D{
		{Key: "explain", Value: bson.D{{Key: "find", Value: query.collection}, {Key: "filter", Value: query.filter}, {Key: "limit", Value: maxRows + 1}}},
		{Key: "verbosity", Value: "queryPlanner"},
		{Key: "maxTimeMS", Value: a.maxTimeMS},
	}
	var planOut bson.M
	if err := db.RunCommand(deadlineFreeContext{ctx}, explain).Decode(&planOut); err != nil {
		return nil, false, mongoReplyError(err, remote)
	}
	*remote = model.QueryExecutionRemoteCompleted
	indexName, ok := mongoIndexGate(planOut)
	if !ok {
		return nil, false, ErrQueryIndexRequired
	}
	find := bson.D{
		{Key: "find", Value: query.collection},
		{Key: "filter", Value: query.filter},
		{Key: "projection", Value: query.cappedProjection()},
		{Key: "limit", Value: maxRows + 1},
		{Key: "singleBatch", Value: true},
		{Key: "maxTimeMS", Value: a.maxTimeMS},
		{Key: "hint", Value: indexName},
		{Key: "comment", Value: mongoComment},
	}
	var out bson.M
	if err := db.RunCommand(deadlineFreeContext{ctx}, find).Decode(&out); err != nil {
		return nil, false, mongoReplyError(err, remote)
	}
	*remote = model.QueryExecutionRemoteCompleted
	cursor := mongoDocM(out["cursor"])
	if cursor == nil {
		return nil, false, ErrQueryBackendFailure
	}
	var cursorID int64 = -1
	switch id := cursor["id"].(type) {
	case int64:
		cursorID = id
	case int32:
		cursorID = int64(id)
	}
	if cursorID != 0 {
		return nil, false, ErrQueryBackendFailure
	}
	batch, _ := cursor["firstBatch"].(bson.A)
	docs := make([]map[string]mongoCell, 0, len(batch))
	for _, item := range batch {
		raw := mongoDocM(item)
		if raw == nil {
			return nil, false, ErrQueryBackendFailure
		}
		doc := make(map[string]mongoCell, len(query.columns))
		for _, name := range query.columns {
			var cell mongoCell
			if over, _ := raw[query.overFlag(name)].(bool); over {
				cell.truncated = true
			} else if value, bad := mongoCellValue(raw[name]); bad {
				cell.truncated = true
			} else {
				cell.value = value
			}
			doc[name] = cell
		}
		docs = append(docs, doc)
	}
	truncated := len(docs) > maxRows
	if truncated {
		docs = docs[:maxRows]
	}
	return docs, truncated, nil
}

// deadlineFreeContext hides the attempt deadline from the driver so it never
// appends its own maxTimeMS — the approval's explicit cap is the only one.
// Cancellation still propagates through Done.
type deadlineFreeContext struct{ context.Context }

func (deadlineFreeContext) Deadline() (time.Time, bool) { return time.Time{}, false }

// mongoReplyError maps a MongoDB command outcome to a controlled sentinel.
// Server-reported errors complete the remote attempt; transport failures and
// deadline-driven selection failures stay at the caller's timeout sentinel.
func mongoReplyError(err error, remote *model.QueryExecutionRemoteState) error {
	var commandErr mongo.CommandError
	if errors.As(err, &commandErr) {
		*remote = model.QueryExecutionRemoteCompleted
		switch commandErr.Code {
		case 50:
			return context.DeadlineExceeded
		case 13:
			return ErrQueryNotAllowed
		}
		return ErrQueryBackendFailure
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	return ErrQueryBackendFailure
}
