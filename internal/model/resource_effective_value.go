// Package model provides CMDB effective-value projections.
package model

type ValueProvenance struct {
	Kind    string `json:"kind"`
	Source  string `json:"source,omitempty"`
	Version uint64 `json:"version,omitempty"`
}

type EffectiveValue struct {
	Value      any             `json:"value"`
	Provenance ValueProvenance `json:"provenance"`
}
