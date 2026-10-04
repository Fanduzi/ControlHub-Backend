// Package service reads PostgreSQL schema metadata through the production pool factory.
// input: context, errors, strings, time, jackc/pgx/v5, internal/model, OpenPostgresPool
// output: livePGSchemaCatalog, withPGSchema, pgSchemaCatalog
// pos: Fixed catalog SQL for T6 metadata; user schema and object names are bind parameters, never SQL text
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
)

// pgSchemaBudget bounds one metadata open, version probe wait, and catalog read.
// The version probe itself still uses the validated ConnConfig ConnectTimeout
// (T3); this budget is the caller waiting on Acquire. statement_timeout below
// bounds the catalog statements after the connection is accepted.
const (
	pgSchemaBudget           = 15 * time.Second
	pgSchemaStatementTimeout = "4000"
)

// pgSchemaCatalog is the PostgreSQL metadata seam. The live implementation
// opens a pool per call. Tests substitute a fake so access and cache behavior
// can be proved without a server. A nil catalog on the service selects live.
type pgSchemaCatalog interface {
	ListFixedDatabase(ctx context.Context, cfg *pgx.ConnConfig, database, q string, page, pageSize int) ([]model.DatabaseSummary, model.PageInfo, error)
	ListSchemas(ctx context.Context, cfg *pgx.ConnConfig, database, q string, page, pageSize int) ([]model.SchemaSummary, model.PageInfo, error)
	ListObjects(ctx context.Context, cfg *pgx.ConnConfig, database, schema, defaultSchema, kind, q string, page, pageSize int) (string, []ObjectSummary, model.PageInfo, error)
	GetObjectDetails(ctx context.Context, cfg *pgx.ConnConfig, database, schema, defaultSchema, name, kind string) (model.ObjectDetailResponse, error)
	GetRelationshipMap(ctx context.Context, cfg *pgx.ConnConfig, database, schema, defaultSchema, name string) (model.RelationshipMapResponse, error)
	ProbeDefinition(ctx context.Context, cfg *pgx.ConnConfig, database, schema, defaultSchema string) error
}

// livePGSchemaCatalog is the production catalog. It has no fields: every call
// borrows a pool from OpenPostgresPool and closes it before returning.
type livePGSchemaCatalog struct{}

const (
	pgSchemaCountSQL = `
SELECT count(*)
FROM pg_catalog.pg_namespace n
WHERE n.nspname <> ALL($1::text[])
  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
  AND ($2::text = '' OR n.nspname ILIKE $3)`

	pgSchemaListSQL = `
SELECT n.nspname
FROM pg_catalog.pg_namespace n
WHERE n.nspname <> ALL($1::text[])
  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
  AND ($2::text = '' OR n.nspname ILIKE $3)
ORDER BY n.nspname
LIMIT $4 OFFSET $5`

	pgObjectCountSQL = `
SELECT count(*)
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND c.relkind::text = ANY($2::text[])
  AND ($3::text = '' OR c.relname ILIKE $4)`

	pgObjectListSQL = `
SELECT c.relname, c.relkind::text
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND c.relkind::text = ANY($2::text[])
  AND ($3::text = '' OR c.relname ILIKE $4)
ORDER BY c.relname
LIMIT $5 OFFSET $6`
)

func pgExcludedSchemas() []string {
	return []string{"pg_catalog", "pg_toast", "information_schema"}
}

// withPGSchema opens one production pool, checks current_database against the
// selected connection, and runs fn inside a read-only transaction.
// Release runs before Close. Rollback runs before Release. fn's sentinel
// errors pass through; driver text does not.
func withPGSchema(ctx context.Context, cfg *pgx.ConnConfig, database string, fn func(context.Context, pgx.Tx) error) error {
	if cfg == nil || database == "" {
		return ErrSchemaBackendError
	}
	ctx, cancel := context.WithTimeout(ctx, pgSchemaBudget)
	defer cancel()

	pool, err := OpenPostgresPool(ctx, cfg)
	if err != nil {
		return mapPGOpenError(err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return mapPGOpenError(err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_catalog.set_config('statement_timeout', $1, false)`, pgSchemaStatementTimeout); err != nil {
		return mapPGQueryError(err)
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return mapPGQueryError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current string
	if err := tx.QueryRow(ctx, `SELECT pg_catalog.current_database()`).Scan(&current); err != nil {
		return mapPGQueryError(err)
	}
	if current != database {
		return ErrSchemaBindingMismatch
	}
	if err := fn(ctx, tx); err != nil {
		return mapPGQueryError(err)
	}
	return nil
}

func mapPGOpenError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPGVersionUnsupported):
		return ErrPGVersionUnsupported
	case errors.Is(err, ErrQueryTimeout), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return ErrSchemaTimeout
	default:
		return ErrSchemaBackendError
	}
}

func mapPGQueryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrSchemaValidationFailed),
		errors.Is(err, ErrSchemaNotFound),
		errors.Is(err, ErrSchemaNotUsable),
		errors.Is(err, ErrQueryObjectNotFound),
		errors.Is(err, ErrSchemaObjectDefinitionUnsupported),
		errors.Is(err, ErrSchemaRelationshipNotSupported),
		errors.Is(err, ErrSchemaBindingMismatch),
		errors.Is(err, ErrSchemaTimeout),
		errors.Is(err, ErrSchemaBackendError),
		errors.Is(err, ErrPGVersionUnsupported):
		return err
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), errors.Is(err, ErrQueryTimeout):
		return ErrSchemaTimeout
	default:
		return ErrSchemaBackendError
	}
}

func mapPGSchemaError(err error) error {
	return mapPGQueryError(err)
}

func (livePGSchemaCatalog) ListFixedDatabase(ctx context.Context, cfg *pgx.ConnConfig, database, q string, page, pageSize int) ([]model.DatabaseSummary, model.PageInfo, error) {
	if err := withPGSchema(ctx, cfg, database, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		return nil, model.PageInfo{}, err
	}
	items, pageInfo := pageFixedDatabase(database, q, page, pageSize)
	return items, pageInfo, nil
}

func pageFixedDatabase(name, q string, page, pageSize int) ([]model.DatabaseSummary, model.PageInfo) {
	page, pageSize = model.NormalizePagination(page, pageSize)
	matched := q == "" || strings.Contains(strings.ToLower(name), strings.ToLower(q))
	total := 0
	items := []model.DatabaseSummary{}
	if matched {
		total = 1
		if (page-1)*pageSize == 0 {
			items = []model.DatabaseSummary{{Name: name, IsDefault: true}}
		}
	}
	return items, model.NewPageInfo(page, pageSize, total)
}

func (livePGSchemaCatalog) ListSchemas(ctx context.Context, cfg *pgx.ConnConfig, database, q string, page, pageSize int) ([]model.SchemaSummary, model.PageInfo, error) {
	page, pageSize, offset := pgPage(page, pageSize)
	var (
		items []model.SchemaSummary
		total int
	)
	err := withPGSchema(ctx, cfg, database, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		items, total, err = pgListSchemas(ctx, tx, q, pageSize, offset)
		return err
	})
	if err != nil {
		return nil, model.PageInfo{}, err
	}
	if items == nil {
		items = []model.SchemaSummary{}
	}
	return items, model.NewPageInfo(page, pageSize, total), nil
}

func pgListSchemas(ctx context.Context, tx pgx.Tx, q string, limit, offset int) ([]model.SchemaSummary, int, error) {
	excluded := pgExcludedSchemas()
	pattern := pgLikePattern(q)
	var total int
	if err := tx.QueryRow(ctx, pgSchemaCountSQL, excluded, q, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, pgSchemaListSQL, excluded, q, pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []model.SchemaSummary{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, 0, err
		}
		items = append(items, model.SchemaSummary{Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (c livePGSchemaCatalog) ListObjects(ctx context.Context, cfg *pgx.ConnConfig, database, schema, defaultSchema, kind, q string, page, pageSize int) (string, []ObjectSummary, model.PageInfo, error) {
	page, pageSize, offset := pgPage(page, pageSize)
	kinds, err := pgRelkinds(kind)
	if err != nil {
		return "", nil, model.PageInfo{}, err
	}
	var (
		pinned string
		items  []ObjectSummary
		total  int
	)
	err = withPGSchema(ctx, cfg, database, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		pinned, err = pgResolveSchema(ctx, tx, schema, defaultSchema)
		if err != nil {
			return err
		}
		items, total, err = pgListObjects(ctx, tx, pinned, kinds, q, pageSize, offset)
		return err
	})
	if err != nil {
		return "", nil, model.PageInfo{}, err
	}
	if items == nil {
		items = []ObjectSummary{}
	}
	return pinned, items, model.NewPageInfo(page, pageSize, total), nil
}

func pgListObjects(ctx context.Context, tx pgx.Tx, schema string, kinds []string, q string, limit, offset int) ([]ObjectSummary, int, error) {
	pattern := pgLikePattern(q)
	var total int
	if err := tx.QueryRow(ctx, pgObjectCountSQL, schema, kinds, q, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, pgObjectListSQL, schema, kinds, q, pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []ObjectSummary{}
	for rows.Next() {
		var name, relkind string
		if err := rows.Scan(&name, &relkind); err != nil {
			return nil, 0, err
		}
		kind, err := pgObjectKind(relkind)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, ObjectSummary{Name: name, Kind: string(kind)})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (livePGSchemaCatalog) GetObjectDetails(ctx context.Context, cfg *pgx.ConnConfig, database, schema, defaultSchema, name, kind string) (model.ObjectDetailResponse, error) {
	kinds, err := pgRelkinds(kind)
	if err != nil {
		return model.ObjectDetailResponse{}, err
	}
	var resp model.ObjectDetailResponse
	err = withPGSchema(ctx, cfg, database, func(ctx context.Context, tx pgx.Tx) error {
		pinned, err := pgResolveSchema(ctx, tx, schema, defaultSchema)
		if err != nil {
			return err
		}
		oid, relkind, err := pgResolveRelation(ctx, tx, pinned, name, kinds)
		if err != nil {
			return err
		}
		built, err := pgObjectDetail(ctx, tx, database, pinned, name, relkind, oid)
		if err != nil {
			return err
		}
		resp = built
		return nil
	})
	if err != nil {
		return model.ObjectDetailResponse{}, err
	}
	resp.EnsureNonNilCollections()
	return resp, nil
}

func (livePGSchemaCatalog) GetRelationshipMap(ctx context.Context, cfg *pgx.ConnConfig, database, schema, defaultSchema, name string) (model.RelationshipMapResponse, error) {
	var resp model.RelationshipMapResponse
	err := withPGSchema(ctx, cfg, database, func(ctx context.Context, tx pgx.Tx) error {
		pinned, err := pgResolveSchema(ctx, tx, schema, defaultSchema)
		if err != nil {
			return err
		}
		oid, relkind, err := pgResolveRelation(ctx, tx, pinned, name, []string{"r", "p", "v"})
		if err != nil {
			return err
		}
		if relkind == "v" {
			return ErrSchemaRelationshipNotSupported
		}
		built, err := pgRelationshipMap(ctx, tx, database, pinned, name, oid)
		if err != nil {
			return err
		}
		resp = built
		return nil
	})
	if err != nil {
		return model.RelationshipMapResponse{}, err
	}
	resp.EnsureNonNilCollections()
	if err := resp.Validate(); err != nil {
		return model.RelationshipMapResponse{}, ErrSchemaBackendError
	}
	return resp, nil
}

func (livePGSchemaCatalog) ProbeDefinition(ctx context.Context, cfg *pgx.ConnConfig, database, schema, defaultSchema string) error {
	return withPGSchema(ctx, cfg, database, func(ctx context.Context, tx pgx.Tx) error {
		_, err := pgResolveSchema(ctx, tx, schema, defaultSchema)
		return err
	})
}

func pgResolveSchema(ctx context.Context, tx pgx.Tx, explicit, fallback string) (string, error) {
	name := explicit
	if name == "" {
		name = fallback
	}
	if name == "" {
		return "", ErrSchemaValidationFailed
	}
	var usage bool
	err := tx.QueryRow(ctx, `
		SELECT pg_catalog.has_schema_privilege(n.oid, 'USAGE')
		FROM pg_catalog.pg_namespace n
		WHERE n.nspname = $1`, name).Scan(&usage)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSchemaNotFound
	}
	if err != nil {
		return "", err
	}
	if !usage {
		return "", ErrSchemaNotUsable
	}
	return name, nil
}

func pgResolveRelation(ctx context.Context, tx pgx.Tx, schema, name string, kinds []string) (uint32, string, error) {
	var oid uint32
	var relkind string
	err := tx.QueryRow(ctx, `
		SELECT c.oid, c.relkind::text
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1
		  AND c.relname = $2
		  AND c.relkind::text = ANY($3::text[])`, schema, name, kinds).Scan(&oid, &relkind)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrQueryObjectNotFound
	}
	if err != nil {
		return 0, "", err
	}
	return oid, relkind, nil
}

func pgObjectDetail(ctx context.Context, tx pgx.Tx, database, schema, name, relkind string, oid uint32) (model.ObjectDetailResponse, error) {
	kind, err := pgObjectKind(relkind)
	if err != nil {
		return model.ObjectDetailResponse{}, err
	}
	cols, colTrunc, err := pgColumns(ctx, tx, oid)
	if err != nil {
		return model.ObjectDetailResponse{}, err
	}
	indexes, idxTrunc, err := pgIndexes(ctx, tx, oid)
	if err != nil {
		return model.ObjectDetailResponse{}, err
	}
	fks, fkTrunc, err := pgForeignKeys(ctx, tx, database, oid)
	if err != nil {
		return model.ObjectDetailResponse{}, err
	}
	return model.ObjectDetailResponse{
		Database:    database,
		Schema:      schema,
		Name:        name,
		Kind:        kind,
		Columns:     cols,
		Indexes:     indexes,
		ForeignKeys: fks,
		Truncated: model.TruncationFlags{
			Columns:     colTrunc,
			Indexes:     idxTrunc,
			ForeignKeys: fkTrunc,
		},
	}, nil
}

func pgColumns(ctx context.Context, tx pgx.Tx, oid uint32) ([]model.ColumnDetail, bool, error) {
	pk, err := pgPrimaryAttnums(ctx, tx, oid)
	if err != nil {
		return nil, false, err
	}
	rows, err := tx.Query(ctx, `
		SELECT a.attname,
		       pg_catalog.format_type(a.atttypid, a.atttypmod),
		       a.attnum,
		       NOT a.attnotnull,
		       a.attidentity::text,
		       pg_catalog.pg_get_expr(d.adbin, d.adrelid)
		FROM pg_catalog.pg_attribute a
		LEFT JOIN pg_catalog.pg_attrdef d
		  ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = $1
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum
		LIMIT $2`, oid, schemaMaxColumns+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	cols := make([]model.ColumnDetail, 0)
	truncated := false
	pos := 0
	for rows.Next() {
		var (
			name, typ, identity string
			attnum              int16
			nullable            bool
			def                 *string
		)
		if err := rows.Scan(&name, &typ, &attnum, &nullable, &identity, &def); err != nil {
			return nil, false, err
		}
		if len(cols) >= schemaMaxColumns {
			truncated = true
			continue
		}
		pos++
		expr := ""
		if def != nil {
			expr = *def
		}
		cols = append(cols, model.ColumnDetail{
			Name:            name,
			DatabaseType:    typ,
			OrdinalPosition: pos,
			Nullable:        nullable,
			PrimaryKey:      pk[attnum],
			AutoIncrement:   pgAutoIncrement(identity, expr),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return cols, truncated, nil
}

func pgPrimaryAttnums(ctx context.Context, tx pgx.Tx, oid uint32) (map[int16]bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT k.attnum
		FROM pg_catalog.pg_index i
		JOIN LATERAL unnest(i.indkey::smallint[]) WITH ORDINALITY AS k(attnum, ord) ON true
		WHERE i.indrelid = $1 AND i.indisprimary AND k.attnum > 0
		ORDER BY k.ord`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int16]bool{}
	for rows.Next() {
		var n int16
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

func pgIndexes(ctx context.Context, tx pgx.Tx, oid uint32) ([]model.IndexDetail, bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT ic.relname,
		       ix.indisunique,
		       ix.indisprimary,
		       cols.ord,
		       cols.attname,
		       cols.keydef
		FROM pg_catalog.pg_index ix
		JOIN pg_catalog.pg_class ic ON ic.oid = ix.indexrelid
		JOIN LATERAL (
			SELECT k.ord::int AS ord,
			       a.attname,
			       pg_catalog.pg_get_indexdef(ix.indexrelid, k.ord::int, true) AS keydef
			FROM unnest(ix.indkey::smallint[]) WITH ORDINALITY AS k(attnum, ord)
			LEFT JOIN pg_catalog.pg_attribute a
			  ON a.attrelid = ix.indrelid AND a.attnum = k.attnum AND k.attnum > 0
		) cols ON true
		WHERE ix.indrelid = $1
		ORDER BY ic.relname, cols.ord`, oid)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var (
		indexes   []model.IndexDetail
		current   *model.IndexDetail
		truncated bool
		count     int
	)
	flush := func() {
		if current == nil {
			return
		}
		if current.Columns == nil {
			current.Columns = []string{}
		}
		indexes = append(indexes, *current)
		current = nil
	}
	for rows.Next() {
		var (
			name, keydef string
			unique, pk   bool
			ord          int
			att          *string
		)
		if err := rows.Scan(&name, &unique, &pk, &ord, &att, &keydef); err != nil {
			return nil, false, err
		}
		if current == nil || current.Name != name {
			flush()
			current = &model.IndexDetail{Name: name, Unique: unique, Primary: pk, Columns: []string{}}
		}
		if count >= schemaMaxIndexColumns {
			truncated = true
			continue
		}
		label := keydef
		if att != nil && *att != "" {
			label = *att
		}
		current.Columns = append(current.Columns, label)
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	flush()
	if indexes == nil {
		indexes = []model.IndexDetail{}
	}
	return indexes, truncated, nil
}

func pgForeignKeys(ctx context.Context, tx pgx.Tx, database string, oid uint32) ([]model.ForeignKeyDetail, bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT con.conname,
		       rn.nspname,
		       rc.relname,
		       con.conkey,
		       con.confkey,
		       con.confupdtype::text,
		       con.confdeltype::text,
		       con.conrelid,
		       con.confrelid
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class rc ON rc.oid = con.confrelid
		JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
		WHERE con.contype = 'f' AND con.conrelid = $1
		ORDER BY con.conname`, oid)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	type pendingFK struct {
		fk pgConstraint
		n  int
	}
	pending := []pendingFK{}
	truncated := false
	pairs := 0
	for rows.Next() {
		fk, n, err := scanPGConstraint(rows)
		if err != nil {
			return nil, false, err
		}
		if pairs >= schemaMaxFKColumnPairs || n > schemaMaxFKColumnPairs-pairs {
			truncated = true
			continue
		}
		pending = append(pending, pendingFK{fk: fk, n: n})
		pairs += n
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	// Column lookup uses the same connection. pgx rejects it while rows are open.
	rows.Close()
	fks := []model.ForeignKeyDetail{}
	for _, item := range pending {
		detail, err := pgConstraintDetail(ctx, tx, database, item.fk)
		if err != nil {
			return nil, false, err
		}
		fks = append(fks, detail)
	}
	return fks, truncated, nil
}

type pgConstraint struct {
	name      string
	refSchema string
	refTable  string
	conKey    []int16
	confKey   []int16
	onUpdate  string
	onDelete  string
	conRel    uint32
	confRel   uint32
	localSch  string
	localName string
}

type pgConstraintScanner interface {
	Scan(dest ...any) error
}

func scanPGConstraint(row pgConstraintScanner) (pgConstraint, int, error) {
	var fk pgConstraint
	if err := row.Scan(&fk.name, &fk.refSchema, &fk.refTable, &fk.conKey, &fk.confKey, &fk.onUpdate, &fk.onDelete, &fk.conRel, &fk.confRel); err != nil {
		return pgConstraint{}, 0, err
	}
	return fk, len(fk.conKey), nil
}

func pgConstraintDetail(ctx context.Context, tx pgx.Tx, database string, fk pgConstraint) (model.ForeignKeyDetail, error) {
	cols, err := pgColumnsInOrder(ctx, tx, fk.conRel, fk.conKey)
	if err != nil {
		return model.ForeignKeyDetail{}, err
	}
	refCols, err := pgColumnsInOrder(ctx, tx, fk.confRel, fk.confKey)
	if err != nil {
		return model.ForeignKeyDetail{}, err
	}
	if len(cols) != len(refCols) || len(cols) == 0 {
		return model.ForeignKeyDetail{}, ErrSchemaBackendError
	}
	return model.ForeignKeyDetail{
		Name:               fk.name,
		Columns:            cols,
		ReferencedDatabase: database,
		ReferencedSchema:   fk.refSchema,
		ReferencedObject:   fk.refTable,
		ReferencedColumns:  refCols,
		OnUpdate:           pgFKAction(fk.onUpdate),
		OnDelete:           pgFKAction(fk.onDelete),
	}, nil
}

func pgRelationshipMap(ctx context.Context, tx pgx.Tx, database, schema, name string, rootOID uint32) (model.RelationshipMapResponse, error) {
	rows, err := tx.Query(ctx, `
		SELECT con.conname,
		       ln.nspname,
		       lc.relname,
		       rn.nspname,
		       rc.relname,
		       con.conkey,
		       con.confkey,
		       con.confupdtype::text,
		       con.confdeltype::text,
		       con.conrelid,
		       con.confrelid
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class lc ON lc.oid = con.conrelid
		JOIN pg_catalog.pg_namespace ln ON ln.oid = lc.relnamespace
		JOIN pg_catalog.pg_class rc ON rc.oid = con.confrelid
		JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
		WHERE con.contype = 'f'
		  AND (con.conrelid = $1 OR con.confrelid = $1)
		ORDER BY con.conname`, rootOID)
	if err != nil {
		return model.RelationshipMapResponse{}, err
	}
	defer rows.Close()

	pending := []pgConstraint{}
	for rows.Next() {
		var fk pgConstraint
		if err := rows.Scan(&fk.name, &fk.localSch, &fk.localName, &fk.refSchema, &fk.refTable, &fk.conKey, &fk.confKey, &fk.onUpdate, &fk.onDelete, &fk.conRel, &fk.confRel); err != nil {
			return model.RelationshipMapResponse{}, err
		}
		pending = append(pending, fk)
	}
	if err := rows.Err(); err != nil {
		return model.RelationshipMapResponse{}, err
	}
	// Column lookup uses the same connection. pgx rejects it while rows are open.
	rows.Close()
	b := pgMapBuilder{
		database: database,
		index:    map[string]string{},
	}
	rootID, ok := b.add(schema, name, model.RelationshipMapRoleRoot)
	if !ok {
		return model.RelationshipMapResponse{}, ErrSchemaBackendError
	}
	for _, fk := range pending {
		if err := b.addEdge(ctx, tx, rootOID, rootID, fk); err != nil {
			return model.RelationshipMapResponse{}, err
		}
	}
	if len(b.nodes) == 0 {
		return model.RelationshipMapResponse{}, ErrSchemaBackendError
	}
	resp := model.RelationshipMapResponse{
		Root:      b.nodes[0],
		Nodes:     b.nodes,
		Edges:     b.edges,
		Truncated: b.truncated,
	}
	if resp.Edges == nil {
		resp.Edges = []model.RelationshipMapEdge{}
	}
	return resp, nil
}

type pgMapBuilder struct {
	database  string
	nodes     []model.RelationshipMapNode
	edges     []model.RelationshipMapEdge
	index     map[string]string
	truncated bool
}

func (b *pgMapBuilder) add(schema, name string, role model.RelationshipMapRole) (string, bool) {
	key := schema + "\x00" + name
	if id, ok := b.index[key]; ok {
		return id, true
	}
	if len(b.nodes) >= model.RelationshipMapMaxNodes {
		b.truncated = true
		return "", false
	}
	id := "n" + pgItoa(len(b.nodes))
	b.nodes = append(b.nodes, model.RelationshipMapNode{
		ID:       id,
		Database: b.database,
		Schema:   schema,
		Name:     name,
		Kind:     model.ObjectKindTable,
		Role:     role,
	})
	b.index[key] = id
	return id, true
}

func (b *pgMapBuilder) addEdge(ctx context.Context, tx pgx.Tx, rootOID uint32, rootID string, fk pgConstraint) error {
	if len(b.edges) >= model.RelationshipMapMaxEdges {
		b.truncated = true
		return nil
	}
	cols, err := pgColumnsInOrder(ctx, tx, fk.conRel, fk.conKey)
	if err != nil {
		return err
	}
	refCols, err := pgColumnsInOrder(ctx, tx, fk.confRel, fk.confKey)
	if err != nil {
		return err
	}
	if len(cols) == 0 || len(cols) != len(refCols) {
		return ErrSchemaBackendError
	}
	var (
		sourceID, targetID string
		direction          model.RelationshipMapDirection
		ok                 bool
	)
	if fk.conRel == rootOID {
		sourceID = rootID
		targetID, ok = b.add(fk.refSchema, fk.refTable, model.RelationshipMapRoleRelated)
		direction = model.RelationshipMapDirectionOutbound
	} else {
		sourceID, ok = b.add(fk.localSch, fk.localName, model.RelationshipMapRoleRelated)
		targetID = rootID
		direction = model.RelationshipMapDirectionInbound
	}
	if !ok {
		b.truncated = true
		return nil
	}
	b.edges = append(b.edges, model.RelationshipMapEdge{
		ID:                "e" + pgItoa(len(b.edges)),
		Direction:         direction,
		SourceID:          sourceID,
		TargetID:          targetID,
		Columns:           cols,
		ReferencedColumns: refCols,
		OnUpdate:          pgFKAction(fk.onUpdate),
		OnDelete:          pgFKAction(fk.onDelete),
	})
	return nil
}

func pgColumnsInOrder(ctx context.Context, tx pgx.Tx, rel uint32, nums []int16) ([]string, error) {
	if len(nums) == 0 {
		return nil, ErrSchemaBackendError
	}
	rows, err := tx.Query(ctx, `
		SELECT a.attnum, a.attname
		FROM pg_catalog.pg_attribute a
		WHERE a.attrelid = $1 AND a.attnum = ANY($2::smallint[])`, rel, nums)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[int16]string{}
	for rows.Next() {
		var n int16
		var name string
		if err := rows.Scan(&n, &name); err != nil {
			return nil, err
		}
		names[n] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(nums))
	for _, n := range nums {
		name, ok := names[n]
		if !ok || n <= 0 {
			return nil, ErrSchemaBackendError
		}
		out = append(out, name)
	}
	return out, nil
}

func pgPage(page, pageSize int) (int, int, int) {
	page, pageSize = model.NormalizePagination(page, pageSize)
	return page, pageSize, (page - 1) * pageSize
}

func pgLikePattern(q string) string {
	if q == "" {
		return "%"
	}
	return "%" + escapePGLike(q) + "%"
}

func escapePGLike(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return q
}

func pgRelkinds(kind string) ([]string, error) {
	switch kind {
	case "":
		return []string{"r", "p", "v"}, nil
	case string(model.ObjectKindTable):
		return []string{"r", "p"}, nil
	case string(model.ObjectKindView):
		return []string{"v"}, nil
	default:
		return nil, ErrSchemaValidationFailed
	}
}

func pgObjectKind(relkind string) (model.ObjectKind, error) {
	switch relkind {
	case "r", "p":
		return model.ObjectKindTable, nil
	case "v":
		return model.ObjectKindView, nil
	default:
		return "", ErrQueryObjectNotFound
	}
}

func pgAutoIncrement(identity, expr string) bool {
	switch strings.TrimSpace(identity) {
	case "a", "d":
		return true
	}
	return strings.Contains(strings.ToLower(expr), "nextval(")
}

func pgFKAction(code string) string {
	switch strings.TrimSpace(code) {
	case "a":
		return "NO ACTION"
	case "r":
		return "RESTRICT"
	case "c":
		return "CASCADE"
	case "n":
		return "SET NULL"
	case "d":
		return "SET DEFAULT"
	default:
		return "NO ACTION"
	}
}

func pgItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
