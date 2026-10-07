// Package mysql provides the MySQL-backed execution-claim protocol store.
// input: context, database/sql, errors, fmt, internal/model claim/execution types
// output: TryClaimExecution, FinalizeClaimWithAudit, GetClaimWithExecution on QueryExecutionRepository + claim sentinels
// pos: G9 atomic occupancy, keyed evidence-pair finalize, and authorized single-snapshot claim read (issue #118, T10-A)
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// Claim-protocol sentinels. Admission refusals are not persistence failures:
// they never increment the evidence-pair failure counter and never write
// history or audit rows.
var (
	// ErrQueryExecutionClaimInvalid marks a malformed admission or finalize
	// input (bad key/digest/actor/scope shape, non-terminal status).
	ErrQueryExecutionClaimInvalid = errors.New("invalid query execution claim")
	// ErrQueryExecutionClaimExists marks a same-key, same-digest admission:
	// the attempt was already recorded; the requester is not the occupant.
	ErrQueryExecutionClaimExists = errors.New("query execution claim already exists")
	// ErrQueryExecutionClaimConflict marks a same-key, different-digest
	// admission: a different request already owns the key.
	ErrQueryExecutionClaimConflict = errors.New("query execution claim conflict")
	// ErrQueryExecutionClaimNotLinked marks a finalize whose (target, key)
	// claim is missing, already linked, or whose actor/connection-scope
	// columns do not match the stored occupancy — the input is not the
	// occupant, so nothing is written.
	ErrQueryExecutionClaimNotLinked = errors.New("query execution claim not linked to this input")
	// ErrQueryExecutionClaimBrokenLink marks a structural violation: a claim
	// whose execution_id is set but whose execution row is absent. Readers get
	// an evidence error, never a fabricated running state or forged id.
	ErrQueryExecutionClaimBrokenLink = errors.New("query execution claim link is broken")
)

const (
	insertExecutionClaimSQL = `INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, actor_machine_principal_id,
		 database_name, schema_name, request_digest, claimed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	selectClaimDigestSQL = `SELECT request_digest FROM query_execution_claims
		WHERE target_resource_id = ? AND client_execution_id = ?`
	// insertKeyedExecutionSQL is the keyed sibling of insertExecutionSQL: the
	// claim protocol additionally persists the resolved connection scope,
	// observed backend handle, remote-state probe, and the idempotency key.
	insertKeyedExecutionSQL = `insert into query_executions
		(target_resource_id, actor_user_id, actor_machine_principal_id, engine, database_name, schema_name,
		 statement_digest, statement_preview, full_statement, status, row_count, duration_ms,
		 error_code, error_message, backend_pid, remote_state, client_execution_id, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(?, CURRENT_TIMESTAMP(6)))`
	// selectClaimForUpdateSQL locks the (target, key) claim row inside the
	// finalize transaction so concurrent finalizers serialize on occupancy:
	// the loser waits for the winner's commit, then sees execution_id set and
	// is refused as a non-occupant — a locking read always reads the latest
	// committed row, so the check cannot run on a stale snapshot.
	selectClaimForUpdateSQL = `SELECT actor_user_id, actor_machine_principal_id,
			database_name, schema_name, execution_id
		FROM query_execution_claims
		WHERE target_resource_id = ? AND client_execution_id = ?
		FOR UPDATE`
	// linkClaimExecutionSQL binds the just-committed terminal execution to the
	// claim only when the claim is still unlinked AND every occupancy field —
	// key, typed actor (NULL-safe), connection scope — matches the stored row.
	// A zero-row result means the input is not the occupant.
	linkClaimExecutionSQL = `UPDATE query_execution_claims SET execution_id = ?
		WHERE target_resource_id = ? AND client_execution_id = ? AND execution_id IS NULL
		  AND actor_user_id <=> ? AND actor_machine_principal_id <=> ?
		  AND database_name = ? AND schema_name = ?`
)

// TryClaimExecution is the single atomic admission gate for a keyed execution
// (G9): one INSERT against the (target_resource_id, client_execution_id)
// primary key decides occupancy. Only the transaction whose INSERT persists
// returns nil — there is no check-then-insert window, no INSERT IGNORE, no
// upsert, no expiry. On a PK conflict the stored digest classifies the
// admission: same digest → ErrQueryExecutionClaimExists, different digest →
// ErrQueryExecutionClaimConflict; a failed conflict read surfaces a safe
// internal error rather than pretending the key is free. The refusal reveals
// nothing about the occupant.
func (r *QueryExecutionRepository) TryClaimExecution(ctx context.Context, in model.QueryExecutionClaimInput) error {
	if err := in.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryExecutionClaimInvalid, err)
	}
	actorUserID, actorMachineID, err := claimActorArgs(in.Identity)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrQueryExecutionClaimInvalid, err)
	}
	if _, err := r.db.ExecContext(ctx, insertExecutionClaimSQL,
		in.TargetResourceID, in.ClientExecutionID, actorUserID, actorMachineID,
		in.DatabaseName, in.SchemaName, in.RequestDigest, in.ClaimedAt.UTC()); err != nil {
		if !isDuplicateKey(err) {
			return fmt.Errorf("insert execution claim: %w", err)
		}
		var stored string
		if qerr := r.db.QueryRowContext(ctx, selectClaimDigestSQL,
			in.TargetResourceID, in.ClientExecutionID).Scan(&stored); qerr != nil {
			return fmt.Errorf("read conflicting execution claim: %w", qerr)
		}
		if stored == in.RequestDigest {
			return ErrQueryExecutionClaimExists
		}
		return ErrQueryExecutionClaimConflict
	}
	return nil
}

// FinalizeClaimWithAudit is the keyed counterpart of InsertExecutionWithAudit:
// the occupant's terminal Execution Evidence Pair — one history row, one fixed
// audit event, and the claim link — commits in a single transaction. The
// history row really persists database_name/schema_name/backend_pid/
// remote_state/client_execution_id; running/unknown are never storable
// statuses. The claim link UPDATE must hit exactly one still-unlinked claim
// whose occupancy fields match this input, or the whole transaction rolls
// back — no evidence pair may persist under an identity it did not claim.
// Any pre-commit failure rolls back history, audit, and link together; the
// claim survives unlinked. A commit failure is reported without retrying.
func (r *QueryExecutionRepository) FinalizeClaimWithAudit(ctx context.Context, rec model.QueryExecutionRecord, eventType, result string) (uint64, error) {
	if rec.ClientExecutionID == nil || *rec.ClientExecutionID == "" {
		return 0, fmt.Errorf("%w: client_execution_id is required for keyed finalize", ErrQueryExecutionClaimInvalid)
	}
	if err := model.ValidateClientExecutionID(*rec.ClientExecutionID); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrQueryExecutionClaimInvalid, err)
	}
	if err := model.ValidateStatus(string(rec.Status)); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrQueryExecutionClaimInvalid, err)
	}
	args, err := keyedExecutionRecordArgs(rec)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrQueryExecutionClaimInvalid, err)
	}
	actorUserID, actorMachineID := args[1], args[2]

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; safe on error

	// Admission gate inside the transaction: the claim must exist, still be
	// unlinked, and belong to this actor/scope — before any history row is
	// staged, so refused finalizes never count as persistence failures.
	var (
		cUser, cMach nullableUint64
		cDB, cSchema string
		cExecID      nullableUint64
	)
	err = tx.QueryRowContext(ctx, selectClaimForUpdateSQL, rec.TargetResourceID, *rec.ClientExecutionID).
		Scan(&cUser, &cMach, &cDB, &cSchema, &cExecID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrQueryExecutionClaimNotLinked
		}
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	if cExecID.Valid || !claimIdentityMatches(cUser, cMach, cDB, cSchema, actorUserID, actorMachineID, rec.DatabaseName, rec.SchemaName) {
		return 0, ErrQueryExecutionClaimNotLinked
	}

	res, err := tx.ExecContext(ctx, insertKeyedExecutionSQL, args...)
	if err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	id, err := res.LastInsertId()
	if err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	if _, err := tx.ExecContext(ctx, insertExecutionAuditSQL, actorUserID, actorMachineID, rec.TargetResourceID, eventType, result); err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	link, err := tx.ExecContext(ctx, linkClaimExecutionSQL, id,
		rec.TargetResourceID, *rec.ClientExecutionID, actorUserID, actorMachineID,
		rec.DatabaseName, rec.SchemaName)
	if err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	n, err := link.RowsAffected()
	if err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	if n != 1 {
		// Not a persistence failure: the claim is missing, already linked, or
		// owned by a different identity. Roll back the staged pair.
		return 0, ErrQueryExecutionClaimNotLinked
	}
	if err := tx.Commit(); err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	return uint64(id), nil
}

// GetClaimWithExecution is the authorized single-snapshot point read: one
// consistent claims LEFT JOIN executions query under the caller's (target,
// database, key, typed actor) scope. A user identity matches only
// actor_user_id with the machine column NULL, and vice versa — user:7 can
// never read machine:7. Unauthorized and nonexistent collapse to
// sql.ErrNoRows; internal failures never masquerade as absent. A linked claim
// returns the real terminal execution; an unlinked claim returns a nil
// Execution; a link that points nowhere is ErrQueryExecutionClaimBrokenLink,
// never a fabricated record.
func (r *QueryExecutionRepository) GetClaimWithExecution(ctx context.Context, targetResourceID uint64, database, clientExecutionID string, actor model.QueryExecutionIdentity) (model.QueryExecutionClaimView, error) {
	if err := actor.Validate(); err != nil {
		return model.QueryExecutionClaimView{}, fmt.Errorf("%w: %v", ErrQueryExecutionClaimInvalid, err)
	}
	actorUserID, actorMachineID, err := claimActorArgs(actor)
	if err != nil {
		return model.QueryExecutionClaimView{}, fmt.Errorf("%w: %v", ErrQueryExecutionClaimInvalid, err)
	}
	const q = `SELECT c.target_resource_id, c.client_execution_id,
			c.actor_user_id, c.actor_machine_principal_id,
			c.database_name, c.schema_name, c.request_digest, c.execution_id, c.claimed_at,
			e.id, e.engine, e.status, e.row_count, e.duration_ms, e.error_code, e.error_message,
			e.database_name, e.schema_name, e.backend_pid, e.remote_state, e.client_execution_id, e.created_at
		FROM query_execution_claims c
		LEFT JOIN query_executions e ON e.id = c.execution_id
		WHERE c.target_resource_id = ? AND c.database_name = ? AND c.client_execution_id = ?
		  AND c.actor_user_id <=> ? AND c.actor_machine_principal_id <=> ?`
	row := r.db.QueryRowContext(ctx, q,
		targetResourceID, database, clientExecutionID, actorUserID, actorMachineID)

	var (
		view         model.QueryExecutionClaimView
		cUser, cMach nullableUint64
		execID       nullableUint64
		// Nullable join columns for the linked execution row.
		eID         nullableUint64
		eEngine     sql.NullString
		eStatus     sql.NullString
		eRowCount   sql.NullInt64
		eDuration   sql.NullInt64
		eErrCode    sql.NullString
		eErrMsg     sql.NullString
		eDB         sql.NullString
		eSchema     sql.NullString
		eBackendPID sql.NullInt64
		eRemote     sql.NullString
		eKey        sql.NullString
		eCreated    sql.NullTime
	)
	err = row.Scan(
		&view.Claim.TargetResourceID, &view.Claim.ClientExecutionID,
		&cUser, &cMach,
		&view.Claim.DatabaseName, &view.Claim.SchemaName,
		&view.Claim.RequestDigest, &execID, &view.Claim.ClaimedAt,
		&eID, &eEngine, &eStatus, &eRowCount, &eDuration, &eErrCode, &eErrMsg,
		&eDB, &eSchema, &eBackendPID, &eRemote, &eKey, &eCreated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.QueryExecutionClaimView{}, sql.ErrNoRows
	}
	if err != nil {
		return model.QueryExecutionClaimView{}, fmt.Errorf("get execution claim view: %w", err)
	}
	if cUser.Valid {
		view.Claim.ActorUserID = cUser.Uint64
	}
	if cMach.Valid {
		view.Claim.ActorMachinePrincipalID = cMach.Uint64
	}
	if !execID.Valid {
		return view, nil // unlinked claim — no derived state fabricated here
	}
	if !eID.Valid {
		return model.QueryExecutionClaimView{}, ErrQueryExecutionClaimBrokenLink
	}
	exec := &model.QueryExecutionRecord{
		ID:               eID.Uint64,
		TargetResourceID: view.Claim.TargetResourceID,
		Engine:           eEngine.String,
		Status:           model.QueryExecutionStatus(eStatus.String),
		DatabaseName:     eDB.String,
		SchemaName:       eSchema.String,
	}
	exec.RowCount = int(eRowCount.Int64)
	exec.DurationMs = eDuration.Int64
	exec.ErrorCode = eErrCode.String
	exec.ErrorMessage = eErrMsg.String
	if eBackendPID.Valid {
		pid := eBackendPID.Int64
		exec.BackendPID = &pid
	}
	exec.RemoteState = model.QueryExecutionRemoteState(eRemote.String)
	if eKey.Valid {
		key := eKey.String
		exec.ClientExecutionID = &key
	}
	if eCreated.Valid {
		exec.CreatedAt = eCreated.Time
	}
	if execID.Uint64 != exec.ID {
		return model.QueryExecutionClaimView{}, ErrQueryExecutionClaimBrokenLink
	}
	view.Claim.ExecutionID = execID.Uint64
	view.Execution = exec
	return view, nil
}

// claimIdentityMatches reports whether the stored occupancy row belongs to
// this finalize input: identical connection scope and identical typed actor
// (NULL-safe on both columns — a machine id can never satisfy a user column).
func claimIdentityMatches(cUser, cMach nullableUint64, cDB, cSchema string, actorUserID, actorMachineID any, database, schema string) bool {
	if cDB != database || cSchema != schema {
		return false
	}
	match := func(stored nullableUint64, want any) bool {
		if want == nil {
			return !stored.Valid
		}
		id, ok := want.(uint64)
		return ok && stored.Valid && stored.Uint64 == id
	}
	return match(cUser, actorUserID) && match(cMach, actorMachineID)
}

// claimActorArgs binds a typed identity to the two NULLable actor columns:
// exactly one side is non-NULL, and a machine id never lands in the user
// column (user:7 ≠ machine:7).
func claimActorArgs(identity model.QueryExecutionIdentity) (any, any, error) {
	if err := identity.Validate(); err != nil {
		return nil, nil, err
	}
	if identity.Kind == model.QueryExecutionActorUser {
		return identity.ID, nil, nil
	}
	return nil, identity.ID, nil
}

// keyedExecutionRecordArgs extends executionRecordArgs with the claim-protocol
// columns persisted only on the keyed path: resolved connection scope,
// observed backend handle, remote-state probe, and the idempotency key. The
// privacy rule is shared: full SQL is stored only for successful user
// executions, never for machine or non-success rows.
func keyedExecutionRecordArgs(rec model.QueryExecutionRecord) ([]any, error) {
	base, err := executionRecordArgs(rec)
	if err != nil {
		return nil, err
	}
	// executionRecordArgs order: target, actor_user, actor_machine, engine,
	// digest, preview, full_statement, status, row_count, duration, error_code,
	// error_message, created_at. The keyed statement inserts database/schema
	// after engine and backend_pid/remote_state/client_execution_id before
	// created_at.
	var backendPID any
	if rec.BackendPID != nil {
		backendPID = *rec.BackendPID
	}
	return []any{
		base[0], base[1], base[2], base[3],
		rec.DatabaseName, rec.SchemaName,
		base[4], base[5], base[6], base[7], base[8], base[9], base[10], base[11],
		backendPID, string(rec.RemoteState), *rec.ClientExecutionID,
		base[12],
	}, nil
}
