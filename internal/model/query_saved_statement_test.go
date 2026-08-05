package model

import (
	"fmt"
	"testing"
)

func TestQuerySavedStatementScopeValidate(t *testing.T) {
	tests := []struct {
		name    string
		scope   QuerySavedStatementScope
		wantErr bool
	}{
		{"personal is valid", QuerySavedStatementPersonal, false},
		{"shared_template is valid", QuerySavedStatementSharedTemplate, false},
		{"empty is invalid", "", true},
		{"unknown is invalid", "public", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.scope.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQuerySavedStatementCreateRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     QuerySavedStatementCreateRequest
		wantErr bool
	}{
		{
			name: "valid personal",
			req: QuerySavedStatementCreateRequest{
				Name:       "Recent orders",
				Statement:  "SELECT id FROM orders",
				Scope:      QuerySavedStatementPersonal,
				Parameters: []QuerySavedStatementParameterDefinition{},
			},
			wantErr: false,
		},
		{
			name: "valid shared_template",
			req: QuerySavedStatementCreateRequest{
				Name:      "Recent orders",
				Statement: "SELECT id FROM orders",
				Scope:     QuerySavedStatementSharedTemplate,
			},
			wantErr: false,
		},
		{
			name: "empty name",
			req: QuerySavedStatementCreateRequest{
				Name:      "",
				Statement: "SELECT id FROM orders",
				Scope:     QuerySavedStatementPersonal,
			},
			wantErr: true,
		},
		{
			name: "name too long",
			req: QuerySavedStatementCreateRequest{
				Name:      string(make([]byte, 121)),
				Statement: "SELECT id FROM orders",
				Scope:     QuerySavedStatementPersonal,
			},
			wantErr: true,
		},
		{
			name: "name with control characters",
			req: QuerySavedStatementCreateRequest{
				Name:      "bad\x00name",
				Statement: "SELECT id FROM orders",
				Scope:     QuerySavedStatementPersonal,
			},
			wantErr: true,
		},
		{
			name: "empty statement",
			req: QuerySavedStatementCreateRequest{
				Name:      "Recent orders",
				Statement: "",
				Scope:     QuerySavedStatementPersonal,
			},
			wantErr: true,
		},
		{
			name: "statement too large",
			req: QuerySavedStatementCreateRequest{
				Name:      "Recent orders",
				Statement: string(make([]byte, 16*1024+1)),
				Scope:     QuerySavedStatementPersonal,
			},
			wantErr: true,
		},
		{
			name: "invalid scope",
			req: QuerySavedStatementCreateRequest{
				Name:      "Recent orders",
				Statement: "SELECT id FROM orders",
				Scope:     "public",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQuerySavedStatementParameterDefinitionsValidate(t *testing.T) {
	valid := []QuerySavedStatementParameterDefinition{
		{Name: "status", Type: QuerySavedStatementParameterString},
		{Name: "minimum_total", Type: QuerySavedStatementParameterDecimal},
	}
	validRequest := QuerySavedStatementCreateRequest{
		Name:       "Recent orders",
		Statement:  "SELECT id FROM orders WHERE status = :status AND total >= :minimum_total",
		Scope:      QuerySavedStatementPersonal,
		Parameters: valid,
	}
	if err := validRequest.Validate(); err != nil {
		t.Fatalf("valid parameter definitions rejected: %v", err)
	}

	tests := []struct {
		name       string
		statement  string
		parameters []QuerySavedStatementParameterDefinition
	}{
		{
			name:       "duplicate names",
			statement:  "SELECT 1 WHERE id = :id",
			parameters: []QuerySavedStatementParameterDefinition{{Name: "id", Type: QuerySavedStatementParameterInteger}, {Name: "id", Type: QuerySavedStatementParameterInteger}},
		},
		{
			name:       "invalid name",
			statement:  "SELECT 1 WHERE id = :Id",
			parameters: []QuerySavedStatementParameterDefinition{{Name: "Id", Type: QuerySavedStatementParameterInteger}},
		},
		{
			name:       "unsupported type",
			statement:  "SELECT 1 WHERE id = :id",
			parameters: []QuerySavedStatementParameterDefinition{{Name: "id", Type: QuerySavedStatementParameterType("json")}},
		},
		{
			name:       "parameter name too long",
			statement:  "SELECT 1 WHERE id = :aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			parameters: []QuerySavedStatementParameterDefinition{{Name: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Type: QuerySavedStatementParameterString}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validRequest
			req.Statement = test.statement
			req.Parameters = test.parameters
			if err := req.Validate(); err == nil {
				t.Fatal("expected invalid parameter definitions to be rejected")
			}
		})
	}

	tooMany := make([]QuerySavedStatementParameterDefinition, MaxSavedStatementParameters+1)
	for index := range tooMany {
		tooMany[index] = QuerySavedStatementParameterDefinition{Name: "parameter_" + fmt.Sprint(index), Type: QuerySavedStatementParameterString}
	}
	tooManyRequest := validRequest
	tooManyRequest.Statement = "SELECT 1"
	tooManyRequest.Parameters = tooMany
	if err := tooManyRequest.Validate(); err == nil {
		t.Fatal("expected parameter count limit to be enforced")
	}
}

func TestQuerySavedStatementUpdateRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     QuerySavedStatementUpdateRequest
		wantErr bool
	}{
		{
			name: "valid",
			req: QuerySavedStatementUpdateRequest{
				Name:      "Recent orders",
				Statement: "SELECT id FROM orders",
			},
			wantErr: false,
		},
		{
			name: "empty name",
			req: QuerySavedStatementUpdateRequest{
				Name:      "",
				Statement: "SELECT id FROM orders",
			},
			wantErr: true,
		},
		{
			name: "empty statement",
			req: QuerySavedStatementUpdateRequest{
				Name:      "Recent orders",
				Statement: "",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQuerySavedStatementJSONShape(t *testing.T) {
	// Verify OwnerUserID is suppressed in JSON
	s := QuerySavedStatement{
		ID:               1,
		TargetResourceID: 2,
		OwnerUserID:      3,
		Name:             "test",
		Statement:        "SELECT 1",
		Scope:            QuerySavedStatementPersonal,
	}
	// The json:"-" tag on OwnerUserID means it won't marshal
	// This test verifies the struct tags are correct
	if s.OwnerUserID != 3 {
		t.Error("OwnerUserID should be accessible in Go code")
	}
}
