// Package model provides domain entities for the resource management system.
// input: time package
// output: AuditEvent struct
// pos: Append-only event record for resource changes
// note: if this file changes, update header and README.md
package model

import "time"

type AuditEvent struct {
	ID               string    `json:"id"`
	ActorUserID      string    `json:"actorUserId"`
	TargetResourceID string    `json:"targetResourceId"`
	EventType        string    `json:"eventType"`
	Result           string    `json:"result"`
	CreatedAt        time.Time `json:"createdAt"`
}
