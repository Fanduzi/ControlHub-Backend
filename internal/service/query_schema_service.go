// Package service provides the QuerySchemaService which orchestrates schema
// metadata introspection: it gates on target access, caches results, calls the
// inspector when needed, and writes audit events for every attempt.
// input: context, errors, fmt, internal/model
// output: QuerySchemaService, NewQuerySchemaService, ErrSchema* sentinels
// pos: Governed schema metadata service with caching and audit
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// Sentinel errors for the schema metadata service. They map to controlled HTTP
// responses and never carry a DSN, host, port, or secret.
var (
	ErrSchemaValidationFailed = errors.New("schema validation failed")
	ErrSchemaNotAllowed       = errors.New("schema access not allowed")
	ErrSchemaTargetNotFound   = errors.New("schema target not found")
	ErrSchemaObjectNotFound   = errors.New("schema object not found")
	ErrSchemaTimeout          = errors.New("schema query timed out")
	ErrSchemaBackendError     = errors.New("schema backend error")
)

// Fixed audit event type strings for schema operations.
const (
	auditSchemaDatabasesListed = "query.schema.databases.listed"
	auditSchemaObjectsListed   = "query.schema.objects.listed"
	auditSchemaObjectRead      = "query.schema.object.read"
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
	q string,
	page, pageSize int,
	includeSystem, refresh bool,
) (model.DatabaseListResponse, error) {
	// 1. Resolve target access.
	bound, err := s.access.Resolve(ctx, actorID, targetID)
	if err != nil {
		return model.DatabaseListResponse{}, s.mapAccessError(err)
	}

	// 2. Check cache (unless refresh).
	key := cacheKey("databases", targetID, bound.Credential.CredentialRef, "", "", q, page, pageSize, includeSystem)
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

// ListObjects returns tables/views for a target database. It resolves target
// access, checks the cache, calls the inspector if needed, writes audit, and
// returns the response.
func (s *QuerySchemaService) ListObjects(
	ctx context.Context,
	actorID, targetID uint64,
	database, kind, q string,
	page, pageSize int,
	refresh bool,
) (model.ObjectListResponse, error) {
	// 1. Resolve target access.
	bound, err := s.access.Resolve(ctx, actorID, targetID)
	if err != nil {
		return model.ObjectListResponse{}, s.mapAccessError(err)
	}

	// 1b. Validate database is not empty.
	if database == "" {
		return model.ObjectListResponse{}, ErrSchemaValidationFailed
	}

	// 2. Check cache (unless refresh).
	key := cacheKey("objects", targetID, bound.Credential.CredentialRef, database, kind, q, page, pageSize, false)
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
			Items:            s.toModelObjects(database, items),
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

// GetObjectDetails returns full column, index, and foreign-key metadata for a
// single table or view.
func (s *QuerySchemaService) GetObjectDetails(
	ctx context.Context,
	actorID, targetID uint64,
	database, name, kind string,
	refresh bool,
) (model.ObjectDetailResponse, error) {
	// 1. Resolve target access.
	bound, err := s.access.Resolve(ctx, actorID, targetID)
	if err != nil {
		return model.ObjectDetailResponse{}, s.mapAccessError(err)
	}

	// 1b. Validate database is not empty.
	if database == "" {
		return model.ObjectDetailResponse{}, ErrSchemaValidationFailed
	}

	// 2. Check cache (unless refresh).
	key := cacheKey("object_details", targetID, bound.Credential.CredentialRef, database, kind, name, 0, 0, false)
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

// mapAccessError maps a target-access error to a controlled schema sentinel.
func (s *QuerySchemaService) mapAccessError(err error) error {
	if errors.Is(err, ErrQueryTargetNotFound) {
		return ErrSchemaTargetNotFound
	}
	// All other access errors (unsupported engine, missing credential, disabled,
	// policy blocked, binding mismatch, secret missing) map to not allowed.
	return ErrSchemaNotAllowed
}

// mapInspectorError maps a raw inspector error to a controlled schema sentinel.
func (s *QuerySchemaService) mapInspectorError(err error) error {
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
func (s *QuerySchemaService) toModelObjects(database string, items []ObjectSummary) []model.ObjectSummary {
	result := make([]model.ObjectSummary, len(items))
	for i, o := range items {
		result[i] = model.ObjectSummary{
			Database: database,
			Name:     o.Name,
			Kind:     model.ObjectKind(o.Kind),
		}
	}
	return result
}

// toModelObjectDetail converts inspector ObjectDetail to model.ObjectDetailResponse.
func (s *QuerySchemaService) toModelObjectDetail(targetID uint64, database string, detail *ObjectDetail) model.ObjectDetailResponse {
	resp := model.ObjectDetailResponse{
		TargetResourceID: int64(targetID),
		Database:         database,
		Name:             detail.Name,
		Kind:             model.ObjectKind(detail.Kind),
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
			Primary: idx.Name == "PRIMARY",
		}
		for _, ic := range idx.Columns {
			md.Columns = append(md.Columns, ic.Name)
		}
		resp.Indexes = append(resp.Indexes, md)
	}
	for _, fk := range detail.ForeignKeys {
		fd := model.ForeignKeyDetail{
			Name:     fk.Name,
			OnUpdate: fk.UpdateRule,
			OnDelete: fk.DeleteRule,
		}
		for _, fc := range fk.Columns {
			fd.Columns = append(fd.Columns, fc.Column)
			fd.ReferencedDatabase = fc.ReferencedSchema
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
	return resp
}
