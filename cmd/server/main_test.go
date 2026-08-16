// Package main provides tests for the ControlHub application entry point
// wiring.
// input: buildDependencies, internal/config
// output: TestBuildDependencies_WiresResourceAndProfileServices
// pos: Proves production DI wires ResourceService and ProfileService for the resource and profile endpoints
// note: if this file changes, update header and README.md
package main

import (
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/config"
)

// TestBuildDependencies_WiresResourceAndProfileServices pins the production
// dependency wiring: the ResourceService (which handles create-with-profile
// atomically through its repository) and the ProfileService (which backs the
// PUT/PATCH/DELETE profile endpoints) must both be present in the router
// dependencies.
func TestBuildDependencies_WiresResourceAndProfileServices(t *testing.T) {
	deps := buildDependencies(nil, config.Config{JWTSecret: strings.Repeat("a", 32)})
	if deps.ResourceService == nil {
		t.Fatal("ResourceService is nil")
	}
	if deps.ProfileService == nil {
		t.Fatal("ProfileService is nil")
	}
}
