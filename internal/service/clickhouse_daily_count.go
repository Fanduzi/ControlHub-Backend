package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/fan/controlhub/internal/model"
)

// chDailyCounts returns seven UTC calendar days, including today up to the request instant.
// The sole input column feeds both WHERE and GROUP BY, so its raw disclosure permission
// must be rechecked even when the client previously saw it in the catalog.
func chDailyCounts(ctx context.Context, conn ch.Conn, a chApproval, catalog chCatalog, req CHPreviewRequest, now time.Time, rec *model.QueryExecutionRecord) (json.RawMessage, int, error) {
	name := req.DailyCountColumn
	typ := catalog.types[req.Table][name]
	if !slices.Contains(a.Tables[req.Table].DailyCountColumns, name) || catalog.modes[req.Table][name] != model.ResultDisclosureRawCopyAllowed || !chTemporal(typ) {
		return nil, 0, ErrQueryDisclosureBlocked
	}
	today := now.UTC().Truncate(24 * time.Hour)
	first := today.AddDate(0, 0, -6)
	col := "src.`" + name + "`"
	day := col
	predicate := fmt.Sprintf("%s BETWEEN toDate('%s') AND toDate('%s')", col, first.Format(time.DateOnly), today.Format(time.DateOnly))
	if chTimestamp(typ) {
		day = "toDate(" + col + ", 'UTC')"
		predicate = fmt.Sprintf("%s BETWEEN toDateTime(%d,'UTC') AND toDateTime(%d,'UTC')", col, first.Unix(), now.Unix())
	}
	query := fmt.Sprintf("SELECT %s AS day, count() AS count FROM `%s`.`%s` AS src WHERE %s GROUP BY %s ORDER BY 1", day, a.Database, req.Table, predicate, day)
	columns := []model.QueryResultColumn{{Name: "day", DatabaseType: "Date", DisplayMode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true}, {Name: "count", DatabaseType: "UInt64", DisplayMode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true}}
	rows, err := readCHProjection(ctx, conn, query, 7, columns, rec)
	if err != nil {
		return nil, 0, err
	}
	// Expand only after a clean terminal result. A failed scan must never look like zero traffic.
	days := make([][]any, 7)
	index := 0
	for i := range days {
		key := first.AddDate(0, 0, i).Format(time.DateOnly)
		days[i] = []any{key, "0"}
		if index < len(rows) && rows[index][0] == key {
			days[i] = rows[index]
			index++
		}
	}
	if index != len(rows) {
		return nil, 0, ErrQueryBackendFailure
	}
	encoded, err := json.Marshal(chPreviewResult{Columns: columns, Rows: days})
	if err != nil {
		return nil, 0, ErrQueryBackendFailure
	}
	return encoded, len(days), nil
}
