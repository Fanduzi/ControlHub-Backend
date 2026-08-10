package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/config"
)

func TestBuildDependencies_WiresProfileServiceIntoResourceService(t *testing.T) {
	deps := buildDependencies(nil, config.Config{JWTSecret: strings.Repeat("a", 32)})
	if deps.ProfileService == nil {
		t.Fatal("ProfileService is nil")
	}

	resourceServiceValue := reflect.ValueOf(deps.ResourceService)
	if !resourceServiceValue.IsValid() || resourceServiceValue.IsNil() {
		t.Fatal("ResourceService is nil")
	}

	profileField := resourceServiceValue.Elem().FieldByName("profileSvc")
	if !profileField.IsValid() {
		t.Fatal("profileSvc field not found")
	}
	if profileField.IsNil() {
		t.Fatal("profileSvc is nil")
	}
}
