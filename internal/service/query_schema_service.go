// Package service provides the QuerySchemaService which orchestrates schema
// metadata introspection: it gates on target access, caches results, calls the
// inspector when needed, and writes audit events for every attempt.
// input: context, errors, fmt, internal/model
// output: QuerySchemaService, NewQuerySchemaService, ObjectDetails, GetObjectDetails, ListSchemas, ErrSchema* sentinels
// pos: Governed schema metadata service with caching and audit
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fan/controlhub/internal/model"
)

// Sentinel errors for the schema metadata service. They map to controlled HTTP
// responses and never carry a DSN, host, port, or secret.
var (
	ErrSchemaValidationFailed            = errors.New("schema validation failed")
	ErrSchemaNotAllowed                  = errors.New("schema access not allowed")
	ErrSchemaTargetNotFound              = errors.New("schema target not found")
	ErrSchemaObjectNotFound              = errors.New("schema object not found")
	ErrSchemaDefinitionNotSupported      = errors.New("schema definition not supported")
	ErrSchemaRelationshipNotSupported    = errors.New("relationship map not supported for this object type")
	ErrSchemaTimeout                     = errors.New("schema query timed out")
	ErrSchemaBackendError                = errors.New("schema backend error")
	ErrSchemaConnectionNotFound          = errors.New("query connection not found")
	ErrSchemaConnectionDisabled          = errors.New("query connection disabled")
	ErrSchemaBindingMismatch             = errors.New("dsn binding mismatch")
	ErrSchemaNotFound                    = errors.New("schema not found")
	ErrSchemaNotUsable                   = errors.New("query schema not usable")
	ErrQueryObjectNotFound               = errors.New("query object not found")
	ErrSchemaObjectDefinitionUnsupported = errors.New("query object definition unsupported")
)

// Fixed audit event type strings for schema operations.
const (
	auditSchemaDatabasesListed     = "query.schema.databases.listed"
	auditSchemaSchemasListed       = "query.schema.schemas.listed"
	auditSchemaObjectsListed       = "query.schema.objects.listed"
	auditSchemaObjectRead          = "query.schema.object.read"
	auditSchemaTableDefinitionRead = "query.schema.table_definition.read"
	auditSchemaRelationshipMapRead = "query.schema.relationship_map.read"
)

// QuerySchemaService orchestrates schema metadata introspection. It resolves
// target access via the shared TargetAccessResolver, caches results in a
// bounded in-memory cache, calls the inspector when needed, and writes one
// audit event per request attempt.
type QuerySchemaService struct {
	access    *TargetAccessResolver
	inspector QuerySchemaInspector
	cache     *QuerySchemaCache
	audit     QueryExecutionRepository
	clock     Clock
	// pgCatalog is nil in production. Tests set it to avoid a live pool.
	pgCatalog pgSchemaCatalog
}

// NewQuerySchemaService wires the service with its dependencies.
func NewQuerySchemaService(
	access *TargetAccessResolver,
	inspector QuerySchemaInspector,
	cache *QuerySchemaCache,
	audit QueryExecutionRepository,
	clock Clock,
) *QuerySchemaService {
	return &QuerySchemaService{
		access:    access,
		inspector: inspector,
		cache:     cache,
		audit:     audit,
		clock:     clock,
	}
}

// ListDatabases returns databases for a target. It resolves target access,
// checks the cache, calls the inspector if needed, writes audit, and returns
// the response.
func (s *QuerySchemaService) ListDatabases(
	ctx context.Context,
	actorID, targetID uint64,
	database, q string,
	page, pageSize int,
	includeSystem, refresh bool,
) (model.DatabaseListResponse, error) {
	// 1. Resolve target access. database selects a PostgreSQL connection.
	// MySQL/TiDB ignore it and keep the legacy empty credential key.
	bound, err := s.access.ResolveMetadata(ctx, actorID, targetID, database)
	if err != nil {
		return model.DatabaseListResponse{}, s.mapAccessError(err)
	}
	if isPGEngine(bound.Target.ConnectionContext.Engine) {
		return s.listPGDatabases(ctx, actorID, targetID, bound, q, page, pageSize, includeSystem, refresh)
	}

	// 2. Check cache (unless refresh). The MySQL key stays database-empty so
	// an optional selector cannot split or collide with the existing entry.
	key := cacheKey("databases", targetID, bound.Credential.CredentialRef, "", "", "", q, page, pageSize, includeSystem)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			// Cache hit — still write audit.
			if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaDatabasesListed, "success"); aErr != nil {
				return model.DatabaseListResponse{}, fmt.Errorf("%w: %v", ErrSchemaBackendError, aErr)
			}
			return cached.(model.DatabaseListResponse), nil
		}
	}

	// 3. Call inspector (with singleflight coalescing).
	var resp model.DatabaseListResponse
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		items, pageInfo, iErr := s.inspector.ListDatabases(ctx, bound.dsn, q, includeSystem, page, pageSize)
		if iErr != nil {
			return nil, iErr
		}
		return model.DatabaseListResponse{
			TargetResourceID: int64(targetID),
			Items:            s.toModelDatabases(items),
			PageInfo:         pageInfo,
		}, nil
	})
	if sfErr != nil {
		// Write audit for failed attempt.
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaDatabasesListed, "failed")
		return model.DatabaseListResponse{}, s.mapInspectorError(sfErr)
	}
	resp = val.(model.DatabaseListResponse)

	// 4. Cache the result.
	s.cache.Set(key, resp)

	// 5. Write audit.
	if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaDatabasesListed, "success"); aErr != nil {
		return model.DatabaseListResponse{}, fmt.Errorf("%w: %v", ErrSchemaBackendError, aErr)
	}

	return resp, nil
}

// ListSchemas returns the user schemas of one PostgreSQL connection.
// database selects that connection. Other engines are rejected.
func (s *QuerySchemaService) ListSchemas(
	ctx context.Context,
	actorID, targetID uint64,
	database, q string,
	page, pageSize int,
	refresh bool,
) (model.SchemaListResponse, error) {
	bound, err := s.access.ResolveMetadata(ctx, actorID, targetID, database)
	if err != nil {
		return model.SchemaListResponse{}, s.mapAccessError(err)
	}
	if !isPGEngine(bound.Target.ConnectionContext.Engine) {
		return model.SchemaListResponse{}, ErrSchemaValidationFailed
	}
	return s.listPGSchemas(ctx, actorID, targetID, bound, q, page, pageSize, refresh)
}

// ListObjects returns tables and views for one database. PostgreSQL also pins
// a schema. MySQL and TiDB reject a non-empty schema.
func (s *QuerySchemaService) ListObjects(
	ctx context.Context,
	actorID, targetID uint64,
	database, schema, kind, q string,
	page, pageSize int,
	refresh bool,
) (model.ObjectListResponse, error) {
	// 1. Resolve target access.
	bound, err := s.access.ResolveMetadata(ctx, actorID, targetID, database)
	if err != nil {
		return model.ObjectListResponse{}, s.mapAccessError(err)
	}
	if isPGEngine(bound.Target.ConnectionContext.Engine) {
		return s.listPGObjects(ctx, actorID, targetID, bound, schema, kind, q, page, pageSize, refresh)
	}

	// 1b. Validate database is not empty. A schema is a PostgreSQL namespace.
	if database == "" || schema != "" {
		return model.ObjectListResponse{}, ErrSchemaValidationFailed
	}

	// 2. Check cache (unless refresh).
	key := cacheKey("objects", targetID, bound.Credential.CredentialRef, database, "", kind, q, page, pageSize, false)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaObjectsListed, "success"); aErr != nil {
				return model.ObjectListResponse{}, fmt.Errorf("%w: %v", ErrSchemaBackendError, aErr)
			}
			return cached.(model.ObjectListResponse), nil
		}
	}

	// 3. Call inspector (with singleflight coalescing).
	var resp model.ObjectListResponse
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		items, pageInfo, iErr := s.inspector.ListObjects(ctx, bound.dsn, database, kind, q, page, pageSize)
		if iErr != nil {
			return nil, iErr
		}
		return model.ObjectListResponse{
			TargetResourceID: int64(targetID),
			Database:         database,
			Items:            s.toModelObjects(database, "", items),
			PageInfo:         pageInfo,
		}, nil
	})
	if sfErr != nil {
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaObjectsListed, "failed")
		return model.ObjectListResponse{}, s.mapInspectorError(sfErr)
	}
	resp = val.(model.ObjectListResponse)

	// 4. Cache the result.
	s.cache.Set(key, resp)

	// 5. Write audit.
	if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaObjectsListed, "success"); aErr != nil {
		return model.ObjectListResponse{}, fmt.Errorf("%w: %v", ErrSchemaBackendError, aErr)
	}

	return resp, nil
}

// ObjectDetails is Schema Inspection for Related Record Navigation: actor and
// target, never a caller-supplied DSN. Inspector errors (including cancel and
// deadline) are returned as-is so navigation can classify them.
func (s *QuerySchemaService) ObjectDetails(ctx context.Context, actorID, targetID uint64, database, name, kind string) (*ObjectDetail, error) {
	bound, err := s.access.Resolve(ctx, actorID, targetID)
	if err != nil {
		return nil, err
	}
	return s.inspector.GetObjectDetails(ctx, bound.dsn, database, name, kind)
}

// GetObjectDetails returns full column, index, and foreign-key metadata for a
// single table or view.
func (s *QuerySchemaService) GetObjectDetails(
	ctx context.Context,
	actorID, targetID uint64,
	database, schema, name, kind string,
	refresh bool,
) (model.ObjectDetailResponse, error) {
	// 1. Resolve target access.
	bound, err := s.access.ResolveMetadata(ctx, actorID, targetID, database)
	if err != nil {
		return model.ObjectDetailResponse{}, s.mapAccessError(err)
	}
	if isPGEngine(bound.Target.ConnectionContext.Engine) {
		return s.getPGObjectDetails(ctx, actorID, targetID, bound, schema, name, kind, refresh)
	}

	// 1b. Validate database is not empty. Schema is PostgreSQL-only.
	if database == "" || schema != "" {
		return model.ObjectDetailResponse{}, ErrSchemaValidationFailed
	}

	// 2. Check cache (unless refresh).
	key := cacheKey("object_details", targetID, bound.Credential.CredentialRef, database, "", kind, name, 0, 0, false)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaObjectRead, "success"); aErr != nil {
				return model.ObjectDetailResponse{}, fmt.Errorf("%w: %v", ErrSchemaBackendError, aErr)
			}
			return cached.(model.ObjectDetailResponse), nil
		}
	}

	// 3. Call inspector (with singleflight coalescing).
	var resp model.ObjectDetailResponse
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		detail, iErr := s.inspector.GetObjectDetails(ctx, bound.dsn, database, name, kind)
		if iErr != nil {
			return nil, iErr
		}
		return s.toModelObjectDetail(targetID, database, detail), nil
	})
	if sfErr != nil {
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaObjectRead, "failed")
		return model.ObjectDetailResponse{}, s.mapInspectorError(sfErr)
	}
	resp = val.(model.ObjectDetailResponse)

	// 4. Cache the result.
	s.cache.Set(key, resp)

	// 5. Write audit.
	if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaObjectRead, "success"); aErr != nil {
		return model.ObjectDetailResponse{}, fmt.Errorf("%w: %v", ErrSchemaBackendError, aErr)
	}

	return resp, nil
}

// GetRelationshipMap returns direct inbound and outbound foreign-key
// relationships for one base table.
func (s *QuerySchemaService) GetRelationshipMap(
	ctx context.Context,
	actorID, targetID uint64,
	database, schema, name string,
	refresh bool,
) (model.RelationshipMapResponse, error) {
	// 1. Resolve target access.
	bound, err := s.access.ResolveMetadata(ctx, actorID, targetID, database)
	if err != nil {
		return model.RelationshipMapResponse{}, s.mapAccessError(err)
	}

	// 2. Validate required parameters.
	if database == "" || name == "" {
		return model.RelationshipMapResponse{}, ErrSchemaValidationFailed
	}
	if isPGEngine(bound.Target.ConnectionContext.Engine) {
		return s.getPGRelationshipMap(ctx, actorID, targetID, bound, schema, name, refresh)
	}
	// Schema is a PostgreSQL namespace. Reject it before the engine gate so
	// TiDB does not report "unsupported" for a parameter it cannot accept.
	if schema != "" {
		return model.RelationshipMapResponse{}, ErrSchemaValidationFailed
	}

	// 2b. Gate by engine: only MySQL is supported for relationship maps.
	if !strings.EqualFold(bound.Target.ConnectionContext.Engine, "mysql") {
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaRelationshipMapRead, "failed")
		return model.RelationshipMapResponse{}, ErrSchemaRelationshipNotSupported
	}

	// 3. Check cache (unless refresh).
	key := cacheKey("relationship_map", targetID, bound.Credential.CredentialRef, database, "", name, "", 0, 0, false)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaRelationshipMapRead, "success"); aErr != nil {
				return model.RelationshipMapResponse{}, ErrSchemaBackendError
			}
			return cached.(model.RelationshipMapResponse), nil
		}
	}

	// 4. Call inspector (with singleflight coalescing).
	var resp model.RelationshipMapResponse
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		result, iErr := s.inspector.GetRelationshipMap(ctx, bound.dsn, database, name)
		if iErr != nil {
			return nil, iErr
		}
		return s.toModelRelationshipMap(int64(targetID), result), nil
	})
	if sfErr != nil {
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaRelationshipMapRead, "failed")
		return model.RelationshipMapResponse{}, s.mapRelationshipMapError(sfErr)
	}
	resp = val.(model.RelationshipMapResponse)

	// 5. Cache the result.
	s.cache.Set(key, resp)

	// 6. Write audit.
	if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaRelationshipMapRead, "success"); aErr != nil {
		return model.RelationshipMapResponse{}, ErrSchemaBackendError
	}

	return resp, nil
}

// GetTableDefinition returns a governed, bounded MySQL SHOW CREATE TABLE
// result for a single verified base table. It resolves target access, verifies
// the object is a BASE TABLE via parameterized information_schema lookup, then
// executes SHOW CREATE TABLE with server-side quoted identifiers. Definition
// text is request-ephemeral: never cached, persisted, logged, or placed in
// query history.
func (s *QuerySchemaService) GetTableDefinition(
	ctx context.Context,
	actorID, targetID uint64,
	database, schema, name string,
) (model.TableDefinitionResponse, error) {
	// 1. Resolve target access.
	bound, err := s.access.ResolveMetadata(ctx, actorID, targetID, database)
	if err != nil {
		return model.TableDefinitionResponse{}, s.mapAccessError(err)
	}

	// 2. Validate required parameters.
	if database == "" || name == "" {
		return model.TableDefinitionResponse{}, ErrSchemaValidationFailed
	}
	if isPGEngine(bound.Target.ConnectionContext.Engine) {
		return s.pgTableDefinition(ctx, actorID, targetID, bound, schema)
	}
	if schema != "" {
		return model.TableDefinitionResponse{}, ErrSchemaValidationFailed
	}

	// 3. Call inspector directly — no cache for table definitions.
	def, iErr := s.inspector.GetTableDefinition(ctx, bound.dsn, database, name)
	if iErr != nil {
		// Write audit for failed attempt.
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaTableDefinitionRead, "failed")
		return model.TableDefinitionResponse{}, s.mapTableDefinitionError(iErr)
	}

	// 4. Write audit for successful attempt.
	if aErr := s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaTableDefinitionRead, "success"); aErr != nil {
		return model.TableDefinitionResponse{}, ErrSchemaBackendError
	}

	// 5. Build response.
	return model.TableDefinitionResponse{
		TargetResourceID: int64(targetID),
		Database:         database,
		Name:             name,
		Kind:             model.ObjectKindTable,
		Dialect:          "mysql",
		Definition:       def.Definition,
		Truncated:        def.Truncated,
	}, nil
}

// mapAccessError maps a target-access error to a controlled schema sentinel.
func (s *QuerySchemaService) mapAccessError(err error) error {
	switch {
	case errors.Is(err, ErrQueryTargetNotFound):
		return ErrSchemaTargetNotFound
	case errors.Is(err, ErrSchemaValidationFailed),
		errors.Is(err, ErrSchemaConnectionNotFound),
		errors.Is(err, ErrSchemaConnectionDisabled),
		errors.Is(err, ErrSchemaBindingMismatch),
		errors.Is(err, ErrSchemaNotFound),
		errors.Is(err, ErrSchemaNotUsable),
		errors.Is(err, ErrPGVersionUnsupported):
		return err
	default:
		// Unsupported engine, missing credential, policy block, and secret
		// failure stay a single not-allowed sentinel. The original text is
		// not forwarded: it can carry a resolver detail.
		return ErrSchemaNotAllowed
	}
}

// mapInspectorError maps a raw inspector error to a controlled schema sentinel.
func (s *QuerySchemaService) mapInspectorError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrSchemaTimeout
	}
	return ErrSchemaBackendError
}

// mapTableDefinitionError maps a raw inspector error from GetTableDefinition
// to a controlled schema sentinel. It preserves ErrSchemaObjectNotFound and
// ErrSchemaDefinitionNotSupported.
func (s *QuerySchemaService) mapTableDefinitionError(err error) error {
	if errors.Is(err, ErrSchemaObjectNotFound) {
		return ErrSchemaObjectNotFound
	}
	if errors.Is(err, ErrSchemaDefinitionNotSupported) {
		return ErrSchemaDefinitionNotSupported
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrSchemaTimeout
	}
	return ErrSchemaBackendError
}

// mapRelationshipMapError maps a raw inspector error from GetRelationshipMap
// to a controlled schema sentinel.
func (s *QuerySchemaService) mapRelationshipMapError(err error) error {
	if errors.Is(err, ErrSchemaDefinitionNotSupported) {
		return ErrSchemaRelationshipNotSupported
	}
	if errors.Is(err, ErrSchemaObjectNotFound) {
		return ErrSchemaObjectNotFound
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrSchemaTimeout
	}
	return ErrSchemaBackendError
}

// toModelDatabases converts inspector DatabaseSummary to model.DatabaseSummary.
func (s *QuerySchemaService) toModelDatabases(items []DatabaseSummary) []model.DatabaseSummary {
	result := make([]model.DatabaseSummary, len(items))
	for i, d := range items {
		result[i] = model.DatabaseSummary{Name: d.Name}
	}
	return result
}

// toModelObjects converts inspector ObjectSummary to model.ObjectSummary.
func (s *QuerySchemaService) toModelObjects(database, schema string, items []ObjectSummary) []model.ObjectSummary {
	result := make([]model.ObjectSummary, len(items))
	for i, o := range items {
		result[i] = model.ObjectSummary{
			Database: database,
			Schema:   schema,
			Name:     o.Name,
			Kind:     model.ObjectKind(o.Kind),
		}
	}
	return result
}

// toModelObjectDetail converts inspector ObjectDetail to model.ObjectDetailResponse.
// All declared collections (including nested index/FK column lists) are non-nil
// empty slices when empty so JSON never emits null for OpenAPI required arrays.
func (s *QuerySchemaService) toModelObjectDetail(targetID uint64, database string, detail *ObjectDetail) model.ObjectDetailResponse {
	resp := model.ObjectDetailResponse{
		TargetResourceID: int64(targetID),
		Database:         database,
		Schema:           detail.Schema,
		Name:             detail.Name,
		Kind:             model.ObjectKind(detail.Kind),
		Columns:          make([]model.ColumnDetail, 0, len(detail.Columns)),
		Indexes:          make([]model.IndexDetail, 0, len(detail.Indexes)),
		ForeignKeys:      make([]model.ForeignKeyDetail, 0, len(detail.ForeignKeys)),
	}
	for _, c := range detail.Columns {
		resp.Columns = append(resp.Columns, model.ColumnDetail{
			Name:            c.Name,
			DatabaseType:    c.Type,
			OrdinalPosition: c.Position,
			Nullable:        c.Nullable == "YES",
			PrimaryKey:      c.Key == "PRI",
			AutoIncrement:   c.Extra == "auto_increment",
		})
	}
	for _, idx := range detail.Indexes {
		md := model.IndexDetail{
			Name:    idx.Name,
			Unique:  !idx.NonUnique,
			Primary: idx.Primary || idx.Name == "PRIMARY",
			Columns: make([]string, 0, len(idx.Columns)),
		}
		for _, ic := range idx.Columns {
			md.Columns = append(md.Columns, ic.Name)
		}
		resp.Indexes = append(resp.Indexes, md)
	}
	for _, fk := range detail.ForeignKeys {
		fd := model.ForeignKeyDetail{
			Name:              fk.Name,
			OnUpdate:          fk.UpdateRule,
			OnDelete:          fk.DeleteRule,
			Columns:           make([]string, 0, len(fk.Columns)),
			ReferencedColumns: make([]string, 0, len(fk.Columns)),
		}
		for _, fc := range fk.Columns {
			fd.Columns = append(fd.Columns, fc.Column)
			fd.ReferencedDatabase = fc.ReferencedSchema
			fd.ReferencedSchema = fc.ReferencedNamespace
			fd.ReferencedObject = fc.ReferencedTable
			fd.ReferencedColumns = append(fd.ReferencedColumns, fc.ReferencedColumn)
		}
		resp.ForeignKeys = append(resp.ForeignKeys, fd)
	}
	if detail.Truncated {
		resp.Truncated = model.TruncationFlags{
			Columns:     len(detail.Columns) >= schemaMaxColumns,
			Indexes:     len(detail.Indexes) >= schemaMaxIndexColumns,
			ForeignKeys: len(detail.ForeignKeys) >= schemaMaxFKColumnPairs,
		}
	}
	resp.EnsureNonNilCollections()
	return resp
}

func (s *QuerySchemaService) toModelRelationshipMap(targetID int64, result *RelationshipMapResult) model.RelationshipMapResponse {
	nodes := make([]model.RelationshipMapNode, len(result.Nodes))
	for i, n := range result.Nodes {
		nodes[i] = model.RelationshipMapNode{
			ID:       n.ID,
			Database: n.Database,
			Schema:   n.Schema,
			Name:     n.Name,
			Kind:     model.ObjectKind(n.Kind),
			Role:     model.RelationshipMapRole(n.Role),
		}
	}
	edges := make([]model.RelationshipMapEdge, len(result.Edges))
	for i, e := range result.Edges {
		cols := make([]string, len(e.Columns))
		copy(cols, e.Columns)
		refCols := make([]string, len(e.ReferencedColumns))
		copy(refCols, e.ReferencedColumns)
		edges[i] = model.RelationshipMapEdge{
			ID:                e.ID,
			Direction:         model.RelationshipMapDirection(e.Direction),
			SourceID:          e.SourceID,
			TargetID:          e.TargetID,
			Columns:           cols,
			ReferencedColumns: refCols,
			OnUpdate:          e.OnUpdate,
			OnDelete:          e.OnDelete,
		}
	}
	resp := model.RelationshipMapResponse{
		TargetResourceID: targetID,
		Root:             nodes[0],
		Nodes:            nodes,
		Edges:            edges,
		Truncated:        result.Truncated,
	}
	resp.EnsureNonNilCollections()
	return resp
}

func (s *QuerySchemaService) postgresCatalog() pgSchemaCatalog {
	if s.pgCatalog != nil {
		return s.pgCatalog
	}
	return livePGSchemaCatalog{}
}

func (s *QuerySchemaService) listPGDatabases(
	ctx context.Context,
	actorID, targetID uint64,
	bound BoundTargetAccess,
	q string,
	page, pageSize int,
	includeSystem, refresh bool,
) (model.DatabaseListResponse, error) {
	if bound.pgConfig == nil {
		return model.DatabaseListResponse{}, ErrSchemaBackendError
	}
	// includeSystem stays in the key. The result is still the one fixed database.
	key := cacheKey("databases", targetID, bound.Credential.CredentialRef, bound.Credential.DatabaseName, "", "", q, page, pageSize, includeSystem)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			if err := s.auditSchema(ctx, actorID, targetID, auditSchemaDatabasesListed, nil); err != nil {
				return model.DatabaseListResponse{}, err
			}
			return cached.(model.DatabaseListResponse), nil
		}
	}
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		items, pageInfo, err := s.postgresCatalog().ListFixedDatabase(ctx, bound.pgConfig, bound.Credential.DatabaseName, q, page, pageSize)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []model.DatabaseSummary{}
		}
		def := bound.Credential.DatabaseName
		return model.DatabaseListResponse{
			TargetResourceID: int64(targetID),
			DefaultDatabase:  &def,
			Items:            items,
			PageInfo:         pageInfo,
		}, nil
	})
	if sfErr != nil {
		return model.DatabaseListResponse{}, s.auditSchema(ctx, actorID, targetID, auditSchemaDatabasesListed, sfErr)
	}
	resp := val.(model.DatabaseListResponse)
	s.cache.Set(key, resp)
	if err := s.auditSchema(ctx, actorID, targetID, auditSchemaDatabasesListed, nil); err != nil {
		return model.DatabaseListResponse{}, err
	}
	return resp, nil
}

func (s *QuerySchemaService) listPGSchemas(
	ctx context.Context,
	actorID, targetID uint64,
	bound BoundTargetAccess,
	q string,
	page, pageSize int,
	refresh bool,
) (model.SchemaListResponse, error) {
	if bound.pgConfig == nil {
		return model.SchemaListResponse{}, ErrSchemaBackendError
	}
	key := cacheKey("schemas", targetID, bound.Credential.CredentialRef, bound.Credential.DatabaseName, "", "", q, page, pageSize, false)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			if err := s.auditSchema(ctx, actorID, targetID, auditSchemaSchemasListed, nil); err != nil {
				return model.SchemaListResponse{}, err
			}
			return cached.(model.SchemaListResponse), nil
		}
	}
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		items, pageInfo, err := s.postgresCatalog().ListSchemas(ctx, bound.pgConfig, bound.Credential.DatabaseName, q, page, pageSize)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []model.SchemaSummary{}
		}
		return model.SchemaListResponse{
			TargetResourceID: int64(targetID),
			Database:         bound.Credential.DatabaseName,
			Items:            items,
			PageInfo:         pageInfo,
		}, nil
	})
	if sfErr != nil {
		return model.SchemaListResponse{}, s.auditSchema(ctx, actorID, targetID, auditSchemaSchemasListed, sfErr)
	}
	resp := val.(model.SchemaListResponse)
	s.cache.Set(key, resp)
	if err := s.auditSchema(ctx, actorID, targetID, auditSchemaSchemasListed, nil); err != nil {
		return model.SchemaListResponse{}, err
	}
	return resp, nil
}

func (s *QuerySchemaService) listPGObjects(
	ctx context.Context,
	actorID, targetID uint64,
	bound BoundTargetAccess,
	schema, kind, q string,
	page, pageSize int,
	refresh bool,
) (model.ObjectListResponse, error) {
	if bound.pgConfig == nil {
		return model.ObjectListResponse{}, ErrSchemaBackendError
	}
	if bound.Credential.DatabaseName == "" {
		return model.ObjectListResponse{}, ErrSchemaValidationFailed
	}
	schemaName, schemaErr := pgEffectiveSchema(schema, bound.Credential.DefaultSchema)
	if schemaErr != nil {
		return model.ObjectListResponse{}, s.auditSchema(ctx, actorID, targetID, auditSchemaObjectsListed, schemaErr)
	}
	key := cacheKey("objects", targetID, bound.Credential.CredentialRef, bound.Credential.DatabaseName, schemaName, kind, q, page, pageSize, false)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			if err := s.auditSchema(ctx, actorID, targetID, auditSchemaObjectsListed, nil); err != nil {
				return model.ObjectListResponse{}, err
			}
			return cached.(model.ObjectListResponse), nil
		}
	}
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		pinned, items, pageInfo, err := s.postgresCatalog().ListObjects(ctx, bound.pgConfig, bound.Credential.DatabaseName, schemaName, bound.Credential.DefaultSchema, kind, q, page, pageSize)
		if err != nil {
			return nil, err
		}
		return model.ObjectListResponse{
			TargetResourceID: int64(targetID),
			Database:         bound.Credential.DatabaseName,
			Schema:           pinned,
			Items:            s.toModelObjects(bound.Credential.DatabaseName, pinned, items),
			PageInfo:         pageInfo,
		}, nil
	})
	if sfErr != nil {
		return model.ObjectListResponse{}, s.auditSchema(ctx, actorID, targetID, auditSchemaObjectsListed, sfErr)
	}
	resp := val.(model.ObjectListResponse)
	s.cache.Set(key, resp)
	if err := s.auditSchema(ctx, actorID, targetID, auditSchemaObjectsListed, nil); err != nil {
		return model.ObjectListResponse{}, err
	}
	return resp, nil
}

func (s *QuerySchemaService) getPGObjectDetails(
	ctx context.Context,
	actorID, targetID uint64,
	bound BoundTargetAccess,
	schema, name, kind string,
	refresh bool,
) (model.ObjectDetailResponse, error) {
	if bound.pgConfig == nil {
		return model.ObjectDetailResponse{}, ErrSchemaBackendError
	}
	if name == "" {
		return model.ObjectDetailResponse{}, ErrSchemaValidationFailed
	}
	schemaName, schemaErr := pgEffectiveSchema(schema, bound.Credential.DefaultSchema)
	if schemaErr != nil {
		return model.ObjectDetailResponse{}, s.auditSchema(ctx, actorID, targetID, auditSchemaObjectRead, schemaErr)
	}
	key := cacheKey("object_details", targetID, bound.Credential.CredentialRef, bound.Credential.DatabaseName, schemaName, kind, name, 0, 0, false)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			if err := s.auditSchema(ctx, actorID, targetID, auditSchemaObjectRead, nil); err != nil {
				return model.ObjectDetailResponse{}, err
			}
			return cached.(model.ObjectDetailResponse), nil
		}
	}
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		resp, err := s.postgresCatalog().GetObjectDetails(ctx, bound.pgConfig, bound.Credential.DatabaseName, schemaName, bound.Credential.DefaultSchema, name, kind)
		if err != nil {
			return nil, err
		}
		resp.TargetResourceID = int64(targetID)
		resp.EnsureNonNilCollections()
		return resp, nil
	})
	if sfErr != nil {
		return model.ObjectDetailResponse{}, s.auditSchema(ctx, actorID, targetID, auditSchemaObjectRead, sfErr)
	}
	resp := val.(model.ObjectDetailResponse)
	s.cache.Set(key, resp)
	if err := s.auditSchema(ctx, actorID, targetID, auditSchemaObjectRead, nil); err != nil {
		return model.ObjectDetailResponse{}, err
	}
	return resp, nil
}

func (s *QuerySchemaService) getPGRelationshipMap(
	ctx context.Context,
	actorID, targetID uint64,
	bound BoundTargetAccess,
	schema, name string,
	refresh bool,
) (model.RelationshipMapResponse, error) {
	if bound.pgConfig == nil {
		return model.RelationshipMapResponse{}, ErrSchemaBackendError
	}
	schemaName, schemaErr := pgEffectiveSchema(schema, bound.Credential.DefaultSchema)
	if schemaErr != nil {
		return model.RelationshipMapResponse{}, s.auditSchema(ctx, actorID, targetID, auditSchemaRelationshipMapRead, schemaErr)
	}
	key := cacheKey("relationship_map", targetID, bound.Credential.CredentialRef, bound.Credential.DatabaseName, schemaName, name, "", 0, 0, false)
	if !refresh {
		if cached, ok := s.cache.Get(key); ok {
			if err := s.auditSchema(ctx, actorID, targetID, auditSchemaRelationshipMapRead, nil); err != nil {
				return model.RelationshipMapResponse{}, err
			}
			return cached.(model.RelationshipMapResponse), nil
		}
	}
	val, sfErr, _ := s.cache.Do(key, func() (any, error) {
		resp, err := s.postgresCatalog().GetRelationshipMap(ctx, bound.pgConfig, bound.Credential.DatabaseName, schemaName, bound.Credential.DefaultSchema, name)
		if err != nil {
			return nil, err
		}
		resp.TargetResourceID = int64(targetID)
		resp.EnsureNonNilCollections()
		return resp, nil
	})
	if sfErr != nil {
		return model.RelationshipMapResponse{}, s.auditSchema(ctx, actorID, targetID, auditSchemaRelationshipMapRead, sfErr)
	}
	resp := val.(model.RelationshipMapResponse)
	s.cache.Set(key, resp)
	if err := s.auditSchema(ctx, actorID, targetID, auditSchemaRelationshipMapRead, nil); err != nil {
		return model.RelationshipMapResponse{}, err
	}
	return resp, nil
}

// pgEffectiveSchema is the namespace used for the cache key, the catalog call,
// and the response. An omitted schema is the connection default. It is not a
// stable empty identity, and it does not fall back to public.
func pgEffectiveSchema(explicit, fallback string) (string, error) {
	name := explicit
	if name == "" {
		name = fallback
	}
	if name == "" {
		return "", ErrSchemaValidationFailed
	}
	return name, nil
}

// pgTableDefinition connects and pins the schema, then refuses definition SQL.
// A down server, missing schema, or unusable schema is not reported as unsupported.
func (s *QuerySchemaService) pgTableDefinition(
	ctx context.Context,
	actorID, targetID uint64,
	bound BoundTargetAccess,
	schema string,
) (model.TableDefinitionResponse, error) {
	if bound.pgConfig == nil {
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaTableDefinitionRead, "failed")
		return model.TableDefinitionResponse{}, ErrSchemaBackendError
	}
	if err := s.postgresCatalog().ProbeDefinition(ctx, bound.pgConfig, bound.Credential.DatabaseName, schema, bound.Credential.DefaultSchema); err != nil {
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaTableDefinitionRead, "failed")
		return model.TableDefinitionResponse{}, mapPGSchemaError(err)
	}
	_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, auditSchemaTableDefinitionRead, "failed")
	return model.TableDefinitionResponse{}, ErrSchemaObjectDefinitionUnsupported
}

func (s *QuerySchemaService) auditSchema(ctx context.Context, actorID, targetID uint64, event string, failed error) error {
	if failed != nil {
		_ = s.audit.InsertAuditEvent(ctx, actorID, targetID, event, "failed")
		return mapPGSchemaError(failed)
	}
	if err := s.audit.InsertAuditEvent(ctx, actorID, targetID, event, "success"); err != nil {
		return ErrSchemaBackendError
	}
	return nil
}
