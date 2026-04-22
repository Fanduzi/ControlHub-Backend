// Package service provides static dictionary service for resource subtype taxonomy.
// input: internal/model (DictionaryItem, ResourceSubtypeDictionary)
// output: NewResourceSubtypeService, ResourceSubtypeService.List
// pos: Static dictionary service for resource subtype taxonomy (reads from in-memory model, no DB)
// note: if this file changes, update header and README.md
package service

import "github.com/fan/controlhub/internal/model"

// ResourceSubtypeService provides subtype dictionary lookups by resource type.
// Unlike other dictionary services, it reads directly from the in-memory model
// and has no repository dependency.
type ResourceSubtypeService struct{}

// NewResourceSubtypeService creates a new ResourceSubtypeService.
func NewResourceSubtypeService() *ResourceSubtypeService {
	return &ResourceSubtypeService{}
}

// List returns the valid subtypes for the given resource type.
// Returns an empty slice if the resource type has no defined subtypes.
func (s *ResourceSubtypeService) List(resourceType string) ([]model.DictionaryItem, error) {
	items := model.ResourceSubtypeDictionary(resourceType)
	if items == nil {
		items = []model.DictionaryItem{}
	}
	return items, nil
}
