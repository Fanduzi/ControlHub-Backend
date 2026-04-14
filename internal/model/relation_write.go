// Package model provides write-side request types for resource relations.
// input: none
// output: RelationCreateInput struct
// pos: Shared request contract for relation create API
// note: if this file changes, update header and README.md
package model

type RelationCreateInput struct {
	FromResourceID string       `json:"-"`
	ToResourceID   string       `json:"toResourceId"`
	RelationType   RelationType `json:"relationType"`
}
