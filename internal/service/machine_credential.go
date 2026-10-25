// Package service implements business logic for resource management.
package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

const machineCredentialPrefix = "chmp_"

type machineCredentialMaterial struct {
	Token    string
	LookupID string
	Hash     [sha256.Size]byte
}

func generateMachineCredential() (machineCredentialMaterial, error) {
	lookup := make([]byte, 12)
	secret := make([]byte, 32)
	if _, err := rand.Read(lookup); err != nil {
		return machineCredentialMaterial{}, fmt.Errorf("generate credential lookup id: %w", err)
	}
	if _, err := rand.Read(secret); err != nil {
		return machineCredentialMaterial{}, fmt.Errorf("generate credential secret: %w", err)
	}
	lookupID := base64.RawURLEncoding.EncodeToString(lookup)
	token := machineCredentialPrefix + lookupID + "." + base64.RawURLEncoding.EncodeToString(secret)
	return machineCredentialMaterial{Token: token, LookupID: lookupID, Hash: sha256.Sum256([]byte(token))}, nil
}

func parseMachineCredential(token string) (string, [sha256.Size]byte, error) {
	if token != strings.TrimSpace(token) || !strings.HasPrefix(token, machineCredentialPrefix) {
		return "", [sha256.Size]byte{}, fmt.Errorf("invalid machine credential")
	}
	lookupID, secret, ok := strings.Cut(strings.TrimPrefix(token, machineCredentialPrefix), ".")
	lookup, lookupErr := base64.RawURLEncoding.DecodeString(lookupID)
	secretBytes, secretErr := base64.RawURLEncoding.DecodeString(secret)
	if !ok || lookupErr != nil || secretErr != nil || len(lookup) != 12 || len(secretBytes) != 32 {
		return "", [sha256.Size]byte{}, fmt.Errorf("invalid machine credential")
	}
	return lookupID, sha256.Sum256([]byte(token)), nil
}
