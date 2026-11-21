package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/fan/controlhub/internal/model"
	"github.com/google/uuid"
)

func (s *QueryExplainService) explainClickHouse(ctx context.Context, user uint64, target model.QueryTarget, req model.ExplainRequest) (model.ExplainResponse, error) {
	attempt, cancel := context.WithTimeout(ctx, defaultQueryTimeout)
	defer cancel()
	var plan NormalizeResult
	lock, err := s.clickHouse.acquire()
	if err == nil {
		defer lock.Close()
		err = s.clickHouse.session(attempt, target, user, func(conn ch.Conn, a chApproval, c chCatalog) error {
			if req.Database != "" || req.Schema != "" || req.MaxRows < 0 {
				return ErrQueryValidationFailed
			}
			compiled, err := compileCHWorksheet(a, c, req.ClickHouseGeneration, req.Statement, req.MaxRows, req.Pagination, nil)
			if err != nil {
				return err
			}
			wire := ch.Context(chQueryContext{attempt}, ch.WithParameters(compiled.params), ch.WithQueryID(uuid.NewString()))
			// Only the plan-producing statement is dispatched; predicates and rows never leave the normalizer.
			rows, err := conn.Query(wire, "EXPLAIN PLAN json=1, indexes=1 "+compiled.sql)
			if err != nil {
				return chExplainFailure(err)
			}
			defer rows.Close()
			if len(rows.Columns()) != 1 {
				return ErrQueryExplainNotSupported
			}
			if !rows.Next() {
				if err := rows.Err(); err != nil {
					return chExplainFailure(err)
				}
				return ErrQueryExplainNotSupported
			}
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return ErrQueryExplainNotSupported
			}
			if len(raw) == 0 || len(raw) > MaxExplainPlanBytes || rows.Next() {
				return ErrQueryExplainNotSupported
			}
			if err := rows.Err(); err != nil {
				return chExplainFailure(err)
			}
			if err := rows.Close(); err != nil {
				return chExplainFailure(err)
			}
			plan, err = normalizeCHExplain(raw)
			return err
		})
	}
	if err != nil && attempt.Err() != nil {
		err = ErrQueryTimeout
		if errors.Is(attempt.Err(), context.Canceled) {
			err = ErrQueryBackendFailure
		}
	}
	outcome := model.ExplainAuditSuccess
	switch {
	case err == nil:
	case errors.Is(err, ErrQueryExplainNotSupported):
		outcome = model.ExplainAuditUnsupported
	case errors.Is(err, ErrQueryPageOutOfRange), errors.Is(err, ErrQueryValidationFailed), errors.Is(err, ErrQueryDisclosureBlocked), errors.Is(err, ErrQueryNotAllowed):
		outcome = model.ExplainAuditRejected
	default:
		outcome = model.ExplainAuditError
	}
	audit, cancelAudit := context.WithTimeout(context.WithoutCancel(ctx), evidencePersistenceWindow)
	defer cancelAudit()
	s.recordAudit(audit, user, target.ResourceID, outcome)
	if err != nil {
		return model.ExplainResponse{}, err
	}
	return model.ExplainResponse{TargetResourceID: target.ResourceID, Engine: model.ExplainEngineClickHouse, FormatVersion: model.ExplainFormatVersion, Nodes: plan.Nodes, Risks: plan.Risks, Truncated: plan.Truncated}, nil
}

func chExplainFailure(err error) error {
	var ex *ch.Exception
	if errors.As(err, &ex) {
		switch ex.Code {
		case 159:
			return ErrQueryTimeout
		case 497:
			return ErrQueryNotAllowed
		case 62, 115:
			return ErrQueryExplainNotSupported
		}
	}
	return ErrQueryBackendFailure
}

// Decode only structural fields. Descriptions, filter conditions, keys and actions may contain user data.
type chExplainNode struct {
	Type    string          `json:"Node Type"`
	Plans   []chExplainNode `json:"Plans"`
	Indexes []struct {
		Type     string
		Initial  json.RawMessage `json:"Initial Granules"`
		Selected json.RawMessage `json:"Selected Granules"`
	} `json:"Indexes"`
}

func normalizeCHExplain(raw string) (NormalizeResult, error) {
	var envelopes []struct{ Plan *chExplainNode }
	if len(raw) > MaxExplainPlanBytes || json.Unmarshal([]byte(raw), &envelopes) != nil || len(envelopes) != 1 || envelopes[0].Plan == nil {
		return NormalizeResult{}, ErrQueryExplainNotSupported
	}
	c := normCtx{nodes: []model.ExplainNode{}, risks: []model.ExplainRisk{}, seenRisks: map[model.ExplainRiskCode]bool{}}
	if err := c.walkCH(*envelopes[0].Plan, "", 0); err != nil {
		return NormalizeResult{}, err
	}
	return NormalizeResult{Nodes: c.nodes, Risks: c.risks, Truncated: c.truncated}, nil
}

func (c *normCtx) walkCH(p chExplainNode, parent string, depth int) error {
	if c.atNodeCap() || depth > MaxExplainPlanDepth {
		c.truncated = true
		return nil
	}
	n := model.ExplainNode{ID: c.allocID(), ParentID: c.maybeParent(parent), Access: model.ExplainAccessUnknown}
	switch p.Type {
	case "":
		return ErrQueryExplainNotSupported
	case "ReadFromMergeTree":
		n.Operation = model.ExplainOpTableAccess
		n.PrimaryKeyGranules = p.granules()
	case "Filter":
		n.Operation = model.ExplainOpFilter
	case "Aggregating", "MergingAggregated":
		n.Operation = model.ExplainOpAggregate
	case "Sorting", "MergeSorting", "PartialSorting":
		n.Operation = model.ExplainOpSort
	case "Expression", "Limit", "LimitBy", "ReadFromPreparedSource", "ReadFromNullSource":
		n.Operation = model.ExplainOpContainer
		n.Label = p.Type
	default:
		n.Operation = model.ExplainOpUnknown
		c.addRisk(model.ExplainRiskUnknownPlanShape, model.ExplainSeverityInfo)
	}
	c.appendNode(n)
	for _, child := range p.Plans {
		if c.atNodeCap() {
			break
		}
		if err := c.walkCH(child, n.ID, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (p chExplainNode) granules() *model.ExplainGranules {
	var pair *model.ExplainGranules
	for _, i := range p.Indexes {
		if i.Type != "PrimaryKey" {
			continue
		}
		initial, e1 := strconv.ParseUint(string(i.Initial), 10, 64)
		selected, e2 := strconv.ParseUint(string(i.Selected), 10, 64)
		if pair != nil || e1 != nil || e2 != nil || selected > initial || initial > maxSafeJSInteger {
			return nil
		}
		pair = &model.ExplainGranules{Selected: selected, Initial: initial}
	}
	return pair
}
