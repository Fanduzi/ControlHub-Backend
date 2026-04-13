// Package model provides domain entities for the resource management system.
// input: time package
// output: ResourceRelation struct
// pos: Directed edge between resources in the relation graph
// note: if this file changes, update header and README.md
package model

import "time"

type ResourceRelation struct {
	ID             string       `json:"id"`
	FromResourceID string       `json:"fromResourceId"`
	ToResourceID   string       `json:"toResourceId"`
	RelationType   RelationType `json:"relationType"`
	CreatedAt      time.Time    `json:"createdAt"`
}
