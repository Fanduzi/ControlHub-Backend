// Package model provides domain entities for the resource management system.
// input: none
// output: UserCredential, LoginRequest, LoginResponse
// pos: Authentication data structures for local login flow
// note: if this file changes, update header and README.md
package model

type UserCredential struct {
	ID           uint64 `json:"id"`
	Email        string `json:"email"`
	RoleName     string `json:"roleName"`
	PasswordHash string `json:"-"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	Role  string `json:"role"`
}
