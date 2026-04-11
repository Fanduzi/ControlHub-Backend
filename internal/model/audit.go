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
