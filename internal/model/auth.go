package model

type UserCredential struct {
	ID           string `json:"id"`
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
