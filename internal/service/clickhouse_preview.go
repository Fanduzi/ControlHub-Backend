package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/fan/controlhub/internal/model"
	"github.com/gofrs/flock"
	"github.com/google/uuid"
)

// CHPreview uses only the single local target sealed by the maintenance controller.
type CHPreview struct {
	dir       string
	execution *QueryExecutionService
	policies  QueryDisclosureReader
}

func NewCHPreview(dir string, execution *QueryExecutionService, policies QueryDisclosureReader) *CHPreview {
	preview := &CHPreview{dir, execution, policies}
	execution.clickHouse = preview
	return preview
}

type CHPreviewRequest struct {
	Generation       string   `json:"generation"`
	Table            string   `json:"table"`
	Columns          []string `json:"columns"`
	Limit            int      `json:"limit"`
	DailyCountColumn string   `json:"dailyCountColumn,omitempty"`
}
type CHPreviewTable struct {
	Worksheet         bool     `json:"worksheet,omitempty"`
	Name              string   `json:"name"`
	Columns           []string `json:"columns"`
	DailyCountColumns []string `json:"dailyCountColumns,omitempty"`
}
type CHPreviewCatalog struct {
	Generation string           `json:"generation"`
	Tables     []CHPreviewTable `json:"tables"`
}
type chApproval struct {
	generation                                          string
	TargetID                                            uint64
	Environment, Address, Database, CredentialRef, User string
	Users                                               []uint64
	Tables                                              map[string]struct {
		UUID              string
		Columns           []string
		DailyCountColumns []string
		Worksheet         bool
	}
}
type chPreviewResult struct {
	Columns []model.QueryResultColumn `json:"columns"`
	Rows    [][]any                   `json:"rows"`
}

type chCatalog struct {
	tables []CHPreviewTable
	types  map[string]map[string]string
	modes  map[string]map[string]model.ResultDisclosureMode
}

var chDateTimeZone = regexp.MustCompile(`^DateTime\('[A-Za-z0-9_+./-]+'\)$`)

func chTimestamp(typ string) bool { return typ == "DateTime" || chDateTimeZone.MatchString(typ) }
func chTemporal(typ string) bool  { return typ == "Date" || chTimestamp(typ) }

var chIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// The driver injects max_execution_time for a visible deadline. Keep Done/Err and the
// server's immutable timeout intact; transport reads and dials have independent bounds.
type chQueryContext struct{ context.Context }

func (chQueryContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (s *CHPreview) acquire() (*flock.Flock, error) {
	info, err := os.Lstat(s.dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return nil, ErrQueryNotAllowed
	}
	for _, name := range []string{"lease", "active.json", "ca.pem"} {
		fi, err := os.Lstat(filepath.Join(s.dir, name))
		if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0022 != 0 || fi.Size() > 65536 {
			return nil, ErrQueryNotAllowed
		}
	}
	lock := flock.New(filepath.Join(s.dir, "lease"), flock.SetFlag(os.O_RDONLY))
	ok, err := lock.TryRLock()
	if err != nil || !ok {
		return nil, ErrQueryNotAllowed
	}
	return lock, nil
}
func (s *CHPreview) approval(target model.QueryTarget, user uint64) (chApproval, error) {
	var a chApproval
	data, err := os.ReadFile(filepath.Join(s.dir, "active.json"))
	if err != nil || len(data) > 65536 || json.Unmarshal(data, &a) != nil {
		return a, ErrQueryNotAllowed
	}
	manifestDigest := sha256.Sum256(data)
	a.generation = fmt.Sprintf("%x", manifestDigest)
	t := target.ConnectionContext
	if t.Engine != "clickhouse" || !chNonProduction(t.Environment) || a.Environment != t.Environment || a.TargetID != target.ResourceID {
		return a, ErrQueryNotAllowed
	}
	if user == 0 || !slices.Contains(a.Users, user) || !net.ParseIP(t.Host).IsLoopback() || a.Address != net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) {
		return a, ErrQueryNotAllowed
	}
	if !chIdentifier.MatchString(a.Database) || !chIdentifier.MatchString(a.User) || model.ValidateCredentialRef(a.CredentialRef) != nil || len(a.Tables) == 0 || len(a.Tables) > 64 {
		return a, ErrQueryNotAllowed
	}
	for name, table := range a.Tables {
		u, e := uuid.Parse(table.UUID)
		if !chIdentifier.MatchString(name) || e != nil || u == uuid.Nil || len(table.Columns) == 0 || len(table.Columns) > 64 {
			return a, ErrQueryNotAllowed
		}
		seen := map[string]bool{}
		for _, c := range table.Columns {
			if !chIdentifier.MatchString(c) || seen[c] {
				return a, ErrQueryNotAllowed
			}
			seen[c] = true
		}
		for _, c := range table.DailyCountColumns {
			if !seen[c] {
				return a, ErrQueryNotAllowed
			}
		}
	}
	return a, nil
}

func chNonProduction(env string) bool {
	return slices.Contains([]string{"Staging", "Development", "Test"}, env)
}

// ApplyWorksheetActions advertises Run only for a separately approved worksheet table
// and a bound credential. Execute always repeats these checks under its own lease.
func (s *CHPreview) ApplyWorksheetActions(ctx context.Context, target *model.QueryTarget, user uint64) {
	if target.ConnectionContext.Engine != "clickhouse" {
		return
	}
	lock, err := s.acquire()
	if err != nil {
		return
	}
	defer lock.Close()
	a, err := s.approval(*target, user)
	if err != nil {
		return
	}
	target.AvailableActions.ClickHousePreview = true
	for _, table := range a.Tables {
		if !table.Worksheet {
			continue
		}
		if _, err := s.password(ctx, *target, a); err != nil {
			return
		}
		ca, err := os.ReadFile(filepath.Join(s.dir, "ca.pem"))
		if err != nil {
			return
		}
		target.ClickHouseGeneration = chGeneration(a, ca)
		target.AvailableActions.Run = true
		target.AvailableActions.SaveSheet = true
		target.AvailableActions.Explain = true
		// The shared grid checks every disclosed cell before serializing the current page.
		target.AvailableActions.Export = true
		target.AvailableActions.ClickHousePaging = true
		target.Readiness = model.ReadinessReady
		target.MissingFields = []string{}
		target.Governance.ExecutionEnabled = true
		target.Governance.SafetyState = model.SafetyStateReadonlySandboxEnabled
		target.Governance.CredentialState = string(model.QueryCredentialRuntimeSecretResolved)
		target.Governance.SafetyNote = "Approved read-only ClickHouse worksheet"
		target.Governance.PolicyNotes = []string{"Non-production, single-table queries only"}
		return
	}
}
func chGeneration(a chApproval, ca []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(append([]byte(a.generation), ca...)))
}

func chCredentialURL(secret, address string) (*url.URL, error) {
	u, err := url.Parse(secret)
	if err != nil || u.Scheme != "clickhouses" || u.Host != address || u.User == nil || !chIdentifier.MatchString(u.User.Username()) || !chIdentifier.MatchString(strings.TrimPrefix(u.Path, "/")) || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, ErrQueryNotAllowed
	}
	if pw, ok := u.User.Password(); !ok || pw == "" {
		return nil, ErrQueryNotAllowed
	}
	return u, nil
}

func (s *CHPreview) password(ctx context.Context, target model.QueryTarget, a chApproval) (string, error) {
	meta, err := s.execution.executions.GetCredential(ctx, target.ResourceID, "")
	if err != nil || !meta.Enabled || meta.EnvironmentPolicy != model.QueryEnvPolicyNonProdOnly || meta.Engine != "clickhouse" || meta.CredentialRef != a.CredentialRef {
		return "", ErrQueryNotAllowed
	}
	secret, err := s.execution.credentials.Resolve(ctx, a.CredentialRef)
	if err != nil {
		return "", ErrQueryNotAllowed
	}
	u, err := chCredentialURL(secret, a.Address)
	if err != nil || u.Path != "/"+a.Database || u.User.Username() != a.User {
		return "", ErrQueryNotAllowed
	}
	password, _ := u.User.Password()
	return password, nil
}

func (s *CHPreview) session(ctx context.Context, target model.QueryTarget, user uint64, fn func(ch.Conn, chApproval, chCatalog) error) error {
	a, err := s.approval(target, user)
	if err != nil {
		return err
	}
	password, err := s.password(ctx, target, a)
	if err != nil {
		return err
	}
	t := target.ConnectionContext
	ca, err := os.ReadFile(filepath.Join(s.dir, "ca.pem"))
	roots := x509.NewCertPool()
	if err != nil || !roots.AppendCertsFromPEM(ca) {
		return ErrQueryNotAllowed
	}
	a.generation = chGeneration(a, ca)
	conn, err := ch.Open(&ch.Options{Addr: []string{a.Address}, Auth: ch.Auth{Database: a.Database, Username: a.User, Password: password}, TLS: &tls.Config{RootCAs: roots, ServerName: t.Host, MinVersion: tls.VersionTLS12}, MaxOpenConns: 1, MaxIdleConns: 1, DialTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, BlockBufferSize: 1, MaxCompressionBuffer: 1 << 20})
	if err != nil {
		return ErrQueryBackendFailure
	}
	defer conn.Close()
	wire := chQueryContext{ctx}
	var ready uint8
	// Refuse accounts without server-side query budgets before reading any table data.
	err = conn.QueryRow(wire, `SELECT toUInt8(currentUser()=? AND currentDatabase()=? AND getSetting('readonly') = 1
        AND getSetting('allow_ddl') = 0 AND getSetting('max_execution_time') BETWEEN 0.001 AND 5
        AND getSetting('max_result_rows') BETWEEN 1 AND 4096 AND getSetting('max_result_bytes') BETWEEN 1 AND 1048576
        AND getSetting('max_rows_to_read') BETWEEN 1 AND 100000 AND getSetting('max_memory_usage') BETWEEN 1 AND 134217728
        AND getSetting('result_overflow_mode') = 'throw' AND getSetting('read_overflow_mode') = 'throw'
        AND getSetting('timeout_overflow_mode') = 'throw')`, a.User, a.Database).Scan(&ready)
	if err != nil {
		return ErrQueryBackendFailure
	}
	if ready != 1 {
		return ErrQueryNotAllowed
	}
	catalog, err := s.catalog(wire, conn, a)
	if err != nil {
		return err
	}
	return fn(conn, a, catalog)
}

func (s *CHPreview) catalog(ctx context.Context, conn ch.Conn, a chApproval) (chCatalog, error) {
	cat := chCatalog{tables: []CHPreviewTable{}, types: map[string]map[string]string{}, modes: map[string]map[string]model.ResultDisclosureMode{}}
	policies, err := s.policies.ListByTarget(ctx, a.TargetID)
	if err != nil {
		return cat, ErrQueryBackendFailure
	}
	names := make([]string, 0, len(a.Tables))
	for name := range a.Tables {
		names = append(names, name)
		cat.modes[name] = map[string]model.ResultDisclosureMode{}
		cat.types[name] = map[string]string{}
	}
	for _, p := range policies {
		if p.TargetResourceID == a.TargetID && p.DatabaseName == a.Database && p.SchemaName == "" && cat.modes[p.ObjectName] != nil && p.Mode.Validate() == nil {
			cat.modes[p.ObjectName][p.ColumnName] = p.Mode
		}
	}
	rows, err := conn.Query(ctx, `SELECT t.name,toString(t.uuid),t.engine,c.name,c.type,c.default_kind FROM system.tables AS t INNER JOIN system.columns AS c ON (t.database,t.name)=(c.database,c.table) WHERE t.database=? AND t.name IN (?) ORDER BY t.name,c.position`, a.Database, names)
	if err != nil {
		return cat, ErrQueryBackendFailure
	}
	defer rows.Close()
	for rows.Next() {
		var table, id, engine, col, typ, kind string
		if err := rows.Scan(&table, &id, &engine, &col, &typ, &kind); err != nil {
			return cat, ErrQueryBackendFailure
		}
		approved, ok := a.Tables[table]
		if !ok || approved.UUID != id || engine != "MergeTree" {
			return cat, ErrQueryDisclosureBlocked
		}
		if kind == "" && slices.Contains(approved.Columns, col) && cat.modes[table][col] != "" && (chTemporal(typ) || slices.Contains([]string{"UInt32", "UInt64", "String", "Nullable(String)"}, typ)) {
			cat.types[table][col] = typ
		}
	}
	if rows.Err() != nil {
		return cat, ErrQueryBackendFailure
	}
	sort.Strings(names)
	for _, name := range names {
		table := CHPreviewTable{Name: name, Worksheet: a.Tables[name].Worksheet}
		for _, col := range a.Tables[name].Columns {
			if cat.types[name][col] != "" {
				table.Columns = append(table.Columns, col)
				if chTemporal(cat.types[name][col]) && cat.modes[name][col] == model.ResultDisclosureRawCopyAllowed && slices.Contains(a.Tables[name].DailyCountColumns, col) {
					table.DailyCountColumns = append(table.DailyCountColumns, col)
				}
			}
		}
		if len(table.Columns) > 0 {
			cat.tables = append(cat.tables, table)
		}
	}
	return cat, nil
}

func (s *CHPreview) Catalog(ctx context.Context, user, targetID uint64) (*CHPreviewCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	target, err := s.execution.access.findTarget(ctx, targetID)
	if err != nil {
		return nil, ErrQueryTargetNotFound
	}
	var catalog *CHPreviewCatalog
	lock, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	err = s.session(ctx, target, user, func(_ ch.Conn, a chApproval, c chCatalog) error {
		catalog = &CHPreviewCatalog{Generation: a.generation, Tables: c.tables}
		return nil
	})
	return catalog, err
}

func (s *CHPreview) Preview(ctx context.Context, user, id uint64, req CHPreviewRequest) (json.RawMessage, error) {
	attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	target, err := s.execution.access.findTarget(attempt, id)
	if err != nil {
		return nil, ErrQueryTargetNotFound
	}
	if target.ConnectionContext.Engine != "clickhouse" {
		return nil, ErrQueryNotAllowed
	}
	started := time.Now()
	rec := model.QueryExecutionRecord{TargetResourceID: id, ActorUserID: user, Engine: "clickhouse"}
	var payload json.RawMessage
	var count int
	lock, err := s.acquire()
	if err == nil {
		defer lock.Close()
		err = s.session(attempt, target, user, func(conn ch.Conn, a chApproval, c chCatalog) error {
			rec.DatabaseName = a.Database
			if req.Generation != a.generation {
				return ErrQueryNotAllowed
			}
			if req.DailyCountColumn != "" {
				if req.Columns != nil || req.Limit != 0 {
					return ErrQueryValidationFailed
				}
				var err error
				payload, count, err = chDailyCounts(attempt, conn, a, c, req, s.execution.clock.Now().UTC(), &rec)
				return err
			}
			if req.Limit < 1 || req.Limit > 20 || len(req.Columns) == 0 || len(req.Columns) > 8 {
				return ErrQueryValidationFailed
			}
			seen := map[string]bool{}
			var plan DisclosurePlan
			var columns []model.QueryResultColumn
			var projection []string
			for _, name := range req.Columns {
				typ := c.types[req.Table][name]
				if typ == "" {
					return ErrQueryDisclosureBlocked
				}
				if seen[name] {
					return ErrQueryValidationFailed
				}
				seen[name] = true
				mode := c.modes[req.Table][name]
				plan.Columns = append(plan.Columns, ColumnDisclosure{Mode: mode, CopyAllowed: mode == model.ResultDisclosureRawCopyAllowed})
				columns = append(columns, model.QueryResultColumn{Name: name, DatabaseType: typ, Nullable: typ == "Nullable(String)"})
				projection = append(projection, "`"+name+"`")
			}
			query := fmt.Sprintf("SELECT %s FROM `%s`.`%s` LIMIT %d", strings.Join(projection, ","), a.Database, req.Table, req.Limit)
			values, readErr := readCHProjection(attempt, conn, query, req.Limit, columns, &rec)
			if readErr != nil {
				return readErr
			}
			cols, values, err := s.execution.disclosure.Apply(plan, columns, values)
			if err != nil {
				return ErrQueryDisclosureBlocked
			}
			encoded, err := json.Marshal(chPreviewResult{Columns: cols, Rows: values})
			if err != nil {
				return ErrQueryBackendFailure
			}
			if len(encoded) > 64<<10 {
				return ErrQueryResultTooLarge
			}
			payload = encoded
			count = len(values)
			return nil
		})
	}
	if err != nil && attempt.Err() != nil {
		err = attempt.Err()
	}
	_, err = s.finishAttempt(ctx, rec, started, count, err)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *CHPreview) finishAttempt(ctx context.Context, rec model.QueryExecutionRecord, started time.Time, count int, err error) (uint64, error) {
	rec.DurationMs = time.Since(started).Milliseconds()
	rec.RowCount = count
	rec.Status = model.QueryExecutionSuccess
	if err != nil {
		var valueErr *TemplateValueValidationError
		switch {
		case errors.As(err, &valueErr):
			rec.Status = model.QueryExecutionRejected
			rec.ErrorCode = "validation_failed"
		case errors.Is(err, ErrQuerySavedStatementNotFound):
			rec.Status = model.QueryExecutionRejected
			rec.ErrorCode = "saved_statement_not_found"
		case errors.Is(err, ErrQueryNotAllowed):
			rec.Status = model.QueryExecutionRejected
			rec.ErrorCode = "query_not_allowed"
		case errors.Is(err, ErrQueryPageOutOfRange):
			rec.Status = model.QueryExecutionRejected
			rec.ErrorCode = "page_out_of_range"
		case errors.Is(err, ErrQueryValidationFailed):
			rec.Status = model.QueryExecutionRejected
			rec.ErrorCode = "validation_failed"
		case errors.Is(err, context.Canceled):
			rec.Status = model.QueryExecutionCancelled
			rec.ErrorCode = "query_canceled"
		default:
			rec.Status, err, rec.ErrorCode, rec.ErrorMessage = classifyExecutorError(err)
		}
	}
	id, saveErr := s.execution.persistEvidencePair(ctx, rec, executionEvidenceQuery)
	if saveErr != nil {
		return 0, ErrQueryBackendFailure
	}
	return id, err
}

func readCHProjection(ctx context.Context, conn ch.Conn, query string, limit int, columns []model.QueryResultColumn, rec *model.QueryExecutionRecord) ([][]any, error) {
	wire := ch.Context(chQueryContext{ctx}, ch.WithQueryID(uuid.NewString()))
	rec.RemoteState = model.QueryExecutionRemoteUnknown
	rows, err := conn.Query(wire, query)
	if err != nil {
		return nil, chTargetError(err, rec)
	}
	defer rows.Close()
	types := rows.ColumnTypes()
	names := rows.Columns()
	if len(names) != len(columns) || len(types) != len(columns) {
		return nil, ErrQueryDisclosureBlocked
	}
	ptr := make([]any, len(columns))
	for i, col := range columns {
		if names[i] != col.Name || types[i].DatabaseTypeName() != col.DatabaseType {
			return nil, ErrQueryDisclosureBlocked
		}
		switch col.DatabaseType {
		case "UInt32":
			ptr[i] = new(uint32)
		case "UInt64":
			ptr[i] = new(uint64)
		case "Date":
			ptr[i] = new(time.Time)
		case "String":
			ptr[i] = new(string)
		case "Nullable(String)":
			ptr[i] = new(*string)
		default:
			if !chTimestamp(col.DatabaseType) {
				return nil, ErrQueryDisclosureBlocked
			}
			ptr[i] = new(time.Time)
		}
	}
	values := [][]any{}
	for rows.Next() {
		if len(values) >= limit {
			return nil, ErrQueryResultTooLarge
		}
		if err := rows.Scan(ptr...); err != nil {
			return nil, ErrQueryBackendFailure
		}
		row := make([]any, len(columns))
		for i, v := range ptr {
			var text *string
			switch p := v.(type) {
			case *uint32:
				row[i] = *p
			case *uint64:
				row[i] = strconv.FormatUint(*p, 10)
			case *time.Time:
				if columns[i].DatabaseType == "Date" {
					row[i] = p.Format(time.DateOnly)
				} else {
					row[i] = p.UTC().Format(time.RFC3339)
				}
			case *string:
				text = p
			case **string:
				text = *p
			}
			if text != nil {
				if len(*text) > 1024 || !utf8.ValidString(*text) {
					return nil, ErrQueryResultTooLarge
				}
				row[i] = strings.Clone(*text)
			}
		}
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		return nil, chTargetError(err, rec)
	}
	if err := rows.Close(); err != nil {
		return nil, chTargetError(err, rec)
	}
	rec.RemoteState = model.QueryExecutionRemoteCompleted
	return values, nil
}

func chTargetError(err error, rec *model.QueryExecutionRecord) error {
	var ex *ch.Exception
	if errors.As(err, &ex) {
		rec.RemoteState = model.QueryExecutionRemoteStopped
		switch ex.Code {
		case 159:
			return context.DeadlineExceeded
		case 158, 396, 241:
			return ErrQueryResultTooLarge
		case 497:
			return ErrQueryNotAllowed
		}
	}
	return ErrQueryBackendFailure
}
