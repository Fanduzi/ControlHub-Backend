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
	// input (bad key/digest/actor/scope shape, non-terminal status, unknown
	// remote-state enum).
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
	// whose execution_id is set but whose execution row is absent or whose
	// stored identity (target, actor, scope, key) does not match the claim.
	// Readers get an evidence error, never a fabricated running state or a
	// forged id.
	ErrQueryExecutionClaimBrokenLink = errors.New("query execution claim link is broken")
	// errQueryExecutionClaimStore is the safe internal failure for claim-store
	// reads/writes: a fixed text with no driver, statement, request, or
	// identity details. Cancellation/deadline stay classifiable through a
	// wrapped ctx error.
	errQueryExecutionClaimStore = errors.New("query execution claim store failed")
)

const (
	insertExecutionClaimSQL = `INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, actor_machine_principal_id,
		 database_name, schema_name, request_digest, claimed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	selectClaimDigestSQL = `SELECT request_digest FROM query_execution_claims
		WHERE target_resource_id = ? AND client_execution_id = ?`
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

// claimStoreError maps a claim-store failure to a safe internal error: raw
// driver/database text never escapes, while caller cancellation and deadlines
// stay classifiable via errors.Is against the bare context sentinel — the
// lower-layer error itself is never re-wrapped, because a wrapped or joined
// context sentinel can carry sensitive text in its message.
func claimStoreError(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("%w: %w", errQueryExecutionClaimStore, cerr)
	}
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%w: %w", errQueryExecutionClaimStore, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", errQueryExecutionClaimStore, context.DeadlineExceeded)
	default:
		return errQueryExecutionClaimStore
	}
}

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
			return claimStoreError(ctx, err)
		}
		var stored string
		if qerr := r.db.QueryRowContext(ctx, selectClaimDigestSQL,
			in.TargetResourceID, in.ClientExecutionID).Scan(&stored); qerr != nil {
			return claimStoreError(ctx, qerr)
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
// remote_state/client_execution_id through the shared executionRecordArgs
// mapping; running/unknown are never storable statuses and remote_state must
// be a known enum. The claim link UPDATE must hit exactly one still-unlinked
// claim whose occupancy fields match this input, or the whole transaction
// rolls back — no evidence pair may persist under an identity it did not
// claim. Any pre-commit failure rolls back history, audit, and link together;
// the claim survives unlinked. A commit failure is reported without retrying.
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
	if err := rec.RemoteState.Validate(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrQueryExecutionClaimInvalid, err)
	}
	args, err := executionRecordArgs(rec)
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

	res, err := tx.ExecContext(ctx, insertExecutionSQL, args...)
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
// returns the real terminal execution after verifying its stored identity —
// target, typed actor, scope, and key — matches the claim; a link that is
// missing or inconsistent is ErrQueryExecutionClaimBrokenLink with an empty
// view, never a fabricated record.
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
			e.id, e.target_resource_id, e.actor_user_id, e.actor_machine_principal_id,
			e.engine, e.status, e.row_count, e.duration_ms, e.error_code, e.error_message,
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
		eID          nullableUint64
		eTarget      nullableUint64
		eUser, eMach nullableUint64
		eEngine      sql.NullString
		eStatus      sql.NullString
		eRowCount    sql.NullInt64
		eDuration    sql.NullInt64
		eErrCode     sql.NullString
		eErrMsg      sql.NullString
		eDB          sql.NullString
		eSchema      sql.NullString
		eBackendPID  sql.NullInt64
		eRemote      sql.NullString
		eKey         sql.NullString
		eCreated     sql.NullTime
	)
	err = row.Scan(
		&view.Claim.TargetResourceID, &view.Claim.ClientExecutionID,
		&cUser, &cMach,
		&view.Claim.DatabaseName, &view.Claim.SchemaName,
		&view.Claim.RequestDigest, &execID, &view.Claim.ClaimedAt,
		&eID, &eTarget, &eUser, &eMach,
		&eEngine, &eStatus, &eRowCount, &eDuration, &eErrCode, &eErrMsg,
		&eDB, &eSchema, &eBackendPID, &eRemote, &eKey, &eCreated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.QueryExecutionClaimView{}, sql.ErrNoRows
	}
	if err != nil {
		return model.QueryExecutionClaimView{}, claimStoreError(ctx, err)
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
	if !eID.Valid || !eTarget.Valid {
		return model.QueryExecutionClaimView{}, ErrQueryExecutionClaimBrokenLink
	}
	// The linked row must be the claim's own terminal evidence: same target,
	// same typed actor, same connection scope, same key. Anything else is a
	// structural violation, not a display state.
	execKeyOK := eKey.Valid && eKey.String == view.Claim.ClientExecutionID
	identityOK := eTarget.Uint64 == view.Claim.TargetResourceID &&
		eDB.String == view.Claim.DatabaseName &&
		eSchema.String == view.Claim.SchemaName &&
		eUser.Valid == cUser.Valid && eMach.Valid == cMach.Valid &&
		(!eUser.Valid || eUser.Uint64 == cUser.Uint64) &&
		(!eMach.Valid || eMach.Uint64 == cMach.Uint64)
	if !execKeyOK || !identityOK {
		return model.QueryExecutionClaimView{}, ErrQueryExecutionClaimBrokenLink
	}
	if err := model.ValidateStatus(eStatus.String); err != nil {
		return model.QueryExecutionClaimView{}, ErrQueryExecutionClaimBrokenLink
	}
	if err := model.QueryExecutionRemoteState(eRemote.String).Validate(); err != nil {
		return model.QueryExecutionClaimView{}, ErrQueryExecutionClaimBrokenLink
	}
	exec := &model.QueryExecutionRecord{
		ID:               eID.Uint64,
		TargetResourceID: eTarget.Uint64,
		Engine:           eEngine.String,
		Status:           model.QueryExecutionStatus(eStatus.String),
		DatabaseName:     eDB.String,
		SchemaName:       eSchema.String,
		RemoteState:      model.QueryExecutionRemoteState(eRemote.String),
		RowCount:         int(eRowCount.Int64),
		DurationMs:       eDuration.Int64,
		ErrorCode:        eErrCode.String,
		ErrorMessage:     eErrMsg.String,
	}
	if eUser.Valid {
		exec.ActorUserID = eUser.Uint64
	}
	if eMach.Valid {
		exec.ActorMachinePrincipalID = eMach.Uint64
	}
	if eBackendPID.Valid {
		pid := eBackendPID.Int64
		exec.BackendPID = &pid
	}
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
