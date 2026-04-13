// Package model provides domain entities for the resource management system.
// input: none
// output: DictionaryItem struct
// pos: Shared schema for all dictionary endpoint responses
// note: if this file changes, update header and README.md
package model

type DictionaryItem struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}
