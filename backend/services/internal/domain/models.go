package domain

import "time"
// ─── Papéis e permissões ────────────────────────────────────────────────────
 
// Role define o papel de um usuário no sistema.
type role string

const (
	Rolepatient role = "patient"
	Rolerescuer role = "rescuer"
	RoleDoctor  role = "doctor"
	RoleAdmin   role = "admin"
)

type User struct {
	ID 	 string   `json:"id"`
	Name string   `json:"name"`
	Email string  `json:"email"`
	Role  Role   `json:"role"`
	CreatedAt time.time `json:"created_at"`
}

