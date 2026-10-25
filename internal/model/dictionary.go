// Package model provides domain entities for the resource management system.
package model

type DictionaryItem struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}
