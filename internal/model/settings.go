package model

type Environment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

type Owner struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
