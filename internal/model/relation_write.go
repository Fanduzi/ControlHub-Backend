// Package model provides write-side request types for resource relations.
// input: none
// output: RelationCreateInput struct
// pos: Shared request contract for relation create API
// note: if this file changes, update header and README.md
package model

type RelationCreateInput struct {
	FromResourceID uint64       `json:"-"`
	ToResourceID   uint64       `json:"toResourceId"`
	RelationType   RelationType `json:"relationType"`
}
