// Package model provides domain entities for the resource management system.
// input: time package
// output: AuditEvent, AuditChange, AuditChangeOperation
// pos: Append-only event record and field-diff contract for governed changes
// note: if this file changes, update header and README.md
package model

import "time"

type AuditChangeOperation string

const (
	AuditChangeAdd    AuditChangeOperation = "add"
	AuditChangeUpdate AuditChangeOperation = "update"
	AuditChangeRemove AuditChangeOperation = "remove"
)

// AuditChange is one safe domain field change. Field names are server-owned;
// write paths must never copy arbitrary request keys into this structure.
type AuditChange struct {
	Field     string               `json:"field"`
	Operation AuditChangeOperation `json:"operation"`
	Before    any                  `json:"before,omitempty"`
	After     any                  `json:"after,omitempty"`
}

// AuditEvent is an append-only record for resource changes and authentication
// outcomes. ActorUserID is nil for unauthenticated events (failed login,
// rejected Bearer) where no verified identity exists.
type AuditEvent struct {
	ID               uint64        `json:"id"`
	ActorUserID      *uint64       `json:"actorUserId"`
	TargetResourceID *uint64       `json:"targetResourceId"`
	EventType        string        `json:"eventType"`
	Result           string        `json:"result"`
	Changes          []AuditChange `json:"changes,omitempty"`
	CreatedAt        time.Time     `json:"createdAt"`
}
