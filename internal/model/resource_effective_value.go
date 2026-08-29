// Package model provides CMDB effective-value projections.
// input: JSON-compatible observed and manual values
// output: EffectiveValue and ValueProvenance read contracts
// pos: Domain projection for observed values with optional manual override
// note: if this file changes, update this header and README.md.
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
