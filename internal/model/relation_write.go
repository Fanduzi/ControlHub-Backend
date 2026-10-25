// Package model provides write-side request types for resource relations.
package model

type RelationCreateInput struct {
	FromResourceID uint64       `json:"-"`
	ToResourceID   uint64       `json:"toResourceId"`
	RelationType   RelationType `json:"relationType"`
}
