package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/fan/controlhub/internal/model"
)

var ErrQueryPageOutOfRange = errors.New("query page is outside the allowed result window")

func (s *CHPreview) executeWorksheet(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	started := time.Now()
	attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rec := model.QueryExecutionRecord{TargetResourceID: target.ResourceID, Engine: "clickhouse", StatementPreview: "ClickHouse worksheet"}
	var out model.QueryExecuteResponse
	run := func() error {
		if identity.Kind != model.QueryExecutionActorUser {
			return ErrQueryNotAllowed
		}
		if req.Database != "" || req.Schema != "" || req.ClientExecutionID != "" {
			return ErrQueryValidationFailed
		}
		return s.session(attempt, target, identity.ID, func(conn ch.Conn, a chApproval, c chCatalog) error {
			compiled, err := compileCHWorksheet(a, c, req.ClickHouseGeneration, req.Statement, req.MaxRows, req.Pagination)
			if a.generation == req.ClickHouseGeneration {
				rec.DatabaseName = a.Database
			}
			if err != nil {
				return err
			}
			// Persist a literal-free shape. Rejected text and predicate values stay private.
			rec.StatementDigest = compiled.digest
			wire := ch.Context(attempt, ch.WithParameters(compiled.params))
			values, err := readCHProjection(wire, conn, compiled.sql, compiled.fetch, compiled.columns, &rec)
			if err != nil {
				return err
			}
			hasMore := len(values) > compiled.pageLimit
			if hasMore {
				values = values[:compiled.pageLimit]
			}
			truncated := hasMore
			var pagination *model.QueryExecutePaginationResponse
			if page := req.Pagination; page != nil {
				pagination = &model.QueryExecutePaginationResponse{Page: page.Page, PageSize: page.PageSize, HasPreviousPage: page.Page > 1, HasNextPage: hasMore}
				truncated = compiled.budgetLimited && compiled.offset+len(values) == compiled.limit && compiled.limit > 0
			}
			cols, values, err := s.execution.disclosure.Apply(compiled.plan, compiled.columns, values)
			if err != nil {
				return ErrQueryDisclosureBlocked
			}
			out = model.QueryExecuteResponse{TargetResourceID: target.ResourceID, Engine: "clickhouse", Status: model.QueryExecutionSuccess, Columns: cols, Rows: values, RowCount: len(values), Truncated: truncated, LimitApplied: compiled.limit, Pagination: pagination, ExecutedAt: started.UTC(), DurationMs: time.Since(started).Milliseconds()}
			// Oversized cells fail the read; every delivered value is complete.
			out.CellTruncated = make([][]bool, len(values))
			for i := range values {
				out.CellTruncated[i] = make([]bool, len(cols))
			}
			// Reserve space for the largest committed execution ID, before committing success.
			out.ExecutionID = ^uint64(0)
			b, err := json.Marshal(out)
			if err != nil {
				return ErrQueryBackendFailure
			}
			if len(b)+1 > 64<<10 {
				return ErrQueryResultTooLarge
			}
			return nil
		})
	}
	lock, err := s.acquire()
	if identity.Kind == model.QueryExecutionActorUser {
		rec.ActorUserID = identity.ID
	} else {
		rec.ActorMachinePrincipalID = identity.ID
	}
	if err == nil {
		defer lock.Close()
		err = run()
	}
	if err != nil && attempt.Err() != nil {
		err = attempt.Err()
	}
	rowCount := 0
	if err == nil {
		rowCount = out.RowCount
	}
	id, err := s.finishAttempt(ctx, rec, started, rowCount, err)
	if err != nil {
		return model.QueryExecuteResponse{}, err
	}
	out.ExecutionID = id
	return out, nil
}

func (s *CHPreview) validateSavedQuery(ctx context.Context, target model.QueryTarget, user uint64, generation, statement string) error {
	if err := (model.QueryExecuteRequest{Statement: statement}).ValidateShape(); err != nil {
		return ErrQueryValidationFailed
	}
	lock, err := s.acquire()
	if err != nil {
		return err
	}
	defer lock.Close()
	return s.session(ctx, target, user, func(_ ch.Conn, a chApproval, c chCatalog) error {
		_, err := compileCHWorksheet(a, c, generation, statement, 0, nil)
		return err
	})
}

// Run and Explain use exactly the same generation, table identity, AST and column policies.
func compileCHWorksheet(a chApproval, c chCatalog, generation, statement string, maxRows int, page *model.QueryExecutePaginationRequest) (*chCompiled, error) {
	if generation != a.generation {
		return nil, ErrQueryNotAllowed
	}
	q, err := parseCHSelect(statement)
	if err != nil {
		return nil, err
	}
	if !a.Tables[q.table].Worksheet {
		return nil, ErrQueryNotAllowed
	}
	if len(c.types[q.table]) == 0 {
		return nil, ErrQueryDisclosureBlocked
	}
	return q.compile(a.Database, c, maxRows, page)
}

type chCompiled struct {
	sql, digest                     string
	params                          ch.Parameters
	columns                         []model.QueryResultColumn
	plan                            DisclosurePlan
	limit, pageLimit, offset, fetch int
	budgetLimited                   bool
}

// Only catalog-validated identifiers and closed grammar constructs are rendered.
// Predicate values travel via typed Native query parameters, separate from SQL.
func (q chSelect) compile(db string, c chCatalog, maxRows int, page *model.QueryExecutePaginationRequest) (*chCompiled, error) {
	result := &chCompiled{limit: 20, params: ch.Parameters{}}
	types, modes := c.types[q.table], c.modes[q.table]
	raw := func(col string) bool { return types[col] != "" && modes[col] == model.ResultDisclosureRawCopyAllowed }
	if q.group != "" && !raw(q.group) {
		return nil, ErrQueryDisclosureBlocked
	}
	if q.order != "" && q.order != q.countAlias && !raw(q.order) {
		return nil, ErrQueryDisclosureBlocked
	}
	var selectList []string
	for _, name := range q.columns {
		typ, mode := types[name], modes[name]
		if q.countAlias != "" && name == q.countAlias {
			typ = "UInt64"
			mode = model.ResultDisclosureRawCopyAllowed
			selectList = append(selectList, "count() AS `"+name+"`")
		} else {
			if typ == "" || mode.Validate() != nil {
				return nil, ErrQueryDisclosureBlocked
			}
			selectList = append(selectList, "src.`"+name+"`")
		}
		result.columns = append(result.columns, model.QueryResultColumn{Name: name, DatabaseType: typ, Nullable: typ == "Nullable(String)"})
		result.plan.Columns = append(result.plan.Columns, ColumnDisclosure{Mode: mode, CopyAllowed: mode == model.ResultDisclosureRawCopyAllowed})
	}
	query := "SELECT " + strings.Join(selectList, ",") + " FROM `" + db + "`.`" + q.table + "` AS src"
	if q.where != nil {
		filter := chPredicate{types: types, modes: modes, params: result.params, countAlias: q.countAlias}
		expr, err := filter.compile(q.where, 0)
		if err != nil {
			return nil, err
		}
		query += " WHERE " + expr
	}
	if q.group != "" {
		query += " GROUP BY src.`" + q.group + "`"
	}
	if q.order != "" {
		if q.order == q.countAlias {
			query += " ORDER BY count()"
		} else {
			query += " ORDER BY src.`" + q.order + "`"
		}
		if q.descending {
			query += " DESC"
		} else {
			query += " ASC"
		}
	}
	result.digest = fmt.Sprintf("%x", sha256.Sum256([]byte(query)))
	if maxRows > 0 {
		result.limit = min(result.limit, maxRows)
	}
	result.budgetLimited = !q.hasLimit || q.limit > uint64(result.limit)
	if !result.budgetLimited {
		result.limit = int(q.limit)
	}
	result.pageLimit = result.limit
	result.fetch = result.limit
	if page == nil {
		// Unpaged callers keep the existing truncation probe.
		if result.budgetLimited {
			result.fetch++
		}
	} else {
		if err := model.ValidatePagination(page.Page, page.PageSize); err != nil {
			return nil, ErrQueryValidationFailed
		}
		result.offset = (page.Page - 1) * page.PageSize
		if page.Page > 1 && result.offset >= result.limit {
			return nil, ErrQueryPageOutOfRange
		}
		remaining := result.limit - result.offset
		result.pageLimit = min(page.PageSize, remaining)
		// A lookahead row is useful only inside the SQL and platform budgets.
		result.fetch = min(result.pageLimit+1, remaining)
	}
	result.sql = query + " LIMIT " + strconv.Itoa(result.fetch)
	if result.offset > 0 {
		result.sql += " OFFSET " + strconv.Itoa(result.offset)
	}
	return result, nil
}

func chFilterValue(name, typ, value string, quoted bool) (string, string, error) {
	if typ == "UInt32" || typ == "UInt64" {
		bits := 64
		if typ == "UInt32" {
			bits = 32
		}
		n, err := strconv.ParseUint(value, 10, bits)
		if quoted || err != nil {
			return "", "", ErrQueryValidationFailed
		}
		return "{" + name + ":" + typ + "}", strconv.FormatUint(n, 10), nil
	}
	if !quoted {
		return "", "", ErrQueryValidationFailed
	}
	if len(value) > 1024 {
		return "", "", ErrQueryValidationFailed
	}
	switch {
	case typ == "String" || typ == "Nullable(String)":
		return "{" + name + ":String}", value, nil
	case typ == "Date":
		if _, err := time.Parse(time.DateOnly, value); err == nil && value >= "1970-01-01" && value <= "2149-06-06" {
			return "{" + name + ":Date}", value, nil
		}
	case chTimestamp(typ):
		t, err := time.Parse(time.RFC3339, value)
		if err == nil && t.Nanosecond() == 0 && t.Unix() >= 0 && t.Unix() <= 4294967295 {
			return "toDateTime({" + name + ":UInt32},'UTC')", strconv.FormatInt(t.Unix(), 10), nil
		}
	}
	return "", "", ErrQueryValidationFailed
}
