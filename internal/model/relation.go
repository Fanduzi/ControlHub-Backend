// Package model provides domain entities for the resource management system.
package model

import "time"

type ResourceRelation struct {
	ID             uint64       `json:"id"`
	FromResourceID uint64       `json:"fromResourceId"`
	ToResourceID   uint64       `json:"toResourceId"`
	RelationType   RelationType `json:"relationType"`
	CreatedAt      time.Time    `json:"createdAt"`
}
