package model

import "time"

type ResourceRelation struct {
	ID             string       `json:"id"`
	FromResourceID string       `json:"fromResourceId"`
	ToResourceID   string       `json:"toResourceId"`
	RelationType   RelationType `json:"relationType"`
	CreatedAt      time.Time    `json:"createdAt"`
}
