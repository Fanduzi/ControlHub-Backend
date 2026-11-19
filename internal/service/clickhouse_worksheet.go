package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/fan/controlhub/internal/model"
)

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
		if req.Database != "" || req.Schema != "" || req.ClientExecutionID != "" || req.Pagination != nil {
			return ErrQueryValidationFailed
		}
		return s.session(attempt, target, identity.ID, func(conn ch.Conn, a chApproval, c chCatalog) error {
			if a.generation != req.ClickHouseGeneration {
				return ErrQueryNotAllowed
			}
			rec.DatabaseName = a.Database
			q, err := parseCHSelect(req.Statement)
			if err != nil {
				return err
			}
			if !a.Tables[q.table].Worksheet {
				return ErrQueryNotAllowed
			}
			if len(c.types[q.table]) == 0 {
				return ErrQueryDisclosureBlocked
			}
			compiled, err := q.compile(a.Database, c, req.MaxRows)
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
			truncated := len(values) > compiled.limit
			if truncated {
				values = values[:compiled.limit]
			}
			cols, values, err := s.execution.disclosure.Apply(compiled.plan, compiled.columns, values)
			if err != nil {
				return ErrQueryDisclosureBlocked
			}
			out = model.QueryExecuteResponse{TargetResourceID: target.ResourceID, Engine: "clickhouse", Status: model.QueryExecutionSuccess, Columns: cols, Rows: values, RowCount: len(values), Truncated: truncated, LimitApplied: compiled.limit, ExecutedAt: started.UTC(), DurationMs: time.Since(started).Milliseconds()}
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

type chCompiled struct {
	sql, digest  string
	params       ch.Parameters
	columns      []model.QueryResultColumn
	plan         DisclosurePlan
	limit, fetch int
}

// Only catalog-validated identifiers and closed grammar constructs are rendered.
// Predicate values travel via typed Native query parameters, separate from SQL.
func (q chSelect) compile(db string, c chCatalog, maxRows int) (*chCompiled, error) {
	result := &chCompiled{limit: 20, params: ch.Parameters{}}
	types, modes := c.types[q.table], c.modes[q.table]
	raw := func(col string) bool { return types[col] != "" && modes[col] == model.ResultDisclosureRawCopyAllowed }
	for _, col := range []string{q.filter, q.group} {
		if col != "" && !raw(col) {
			return nil, ErrQueryDisclosureBlocked
		}
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
	if q.filter != "" {
		expression, value, err := chFilterValue(types[q.filter], q.literal)
		if err != nil {
			return nil, err
		}
		result.params["worksheet_value"] = value
		query += " WHERE src.`" + q.filter + "` = " + expression
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
	result.fetch = result.limit + 1
	if q.hasLimit && q.limit <= uint64(result.limit) {
		result.limit = int(q.limit)
		result.fetch = result.limit
	}
	result.sql = query + " LIMIT " + strconv.Itoa(result.fetch)
	return result, nil
}

func chFilterValue(typ, literal string) (string, string, error) {
	quoted := strings.HasPrefix(literal, "'")
	if typ == "UInt32" || typ == "UInt64" {
		bits := 64
		if typ == "UInt32" {
			bits = 32
		}
		n, err := strconv.ParseUint(literal, 10, bits)
		if quoted || err != nil {
			return "", "", ErrQueryValidationFailed
		}
		return "{worksheet_value:" + typ + "}", strconv.FormatUint(n, 10), nil
	}
	if !quoted {
		return "", "", ErrQueryValidationFailed
	}
	value := strings.ReplaceAll(literal[1:len(literal)-1], "''", "'")
	if len(value) > 1024 {
		return "", "", ErrQueryValidationFailed
	}
	switch {
	case typ == "String" || typ == "Nullable(String)":
		return "{worksheet_value:String}", value, nil
	case typ == "Date":
		if _, err := time.Parse(time.DateOnly, value); err == nil && value >= "1970-01-01" && value <= "2149-06-06" {
			return "{worksheet_value:Date}", value, nil
		}
	case chTimestamp(typ):
		t, err := time.Parse(time.RFC3339, value)
		if err == nil && t.Nanosecond() == 0 && t.Unix() >= 0 && t.Unix() <= 4294967295 {
			return "toDateTime({worksheet_value:UInt32},'UTC')", strconv.FormatInt(t.Unix(), 10), nil
		}
	}
	return "", "", ErrQueryValidationFailed
}
