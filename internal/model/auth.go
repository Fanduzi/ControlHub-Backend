// Package model provides domain entities for the resource management system.
package model

// UserCredential is the durable server-owned authentication and authorization
// state for one user. AuthorizationVersion and IsActive are the authoritative
// inputs for Backend Bearer Credential acceptance; a role embedded in a signed
// token is never authoritative.
type UserCredential struct {
	ID                   uint64 `json:"id"`
	Email                string `json:"email"`
	RoleName             string `json:"roleName"`
	PasswordHash         string `json:"-"`
	IsActive             bool   `json:"-"`
	AuthorizationVersion uint64 `json:"-"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token  string `json:"token"`
	Role   string `json:"role"`
	UserID uint64 `json:"userId"`
}
