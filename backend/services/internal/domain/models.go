package domain

import "time"

// ─── Papéis e permissões ────────────────────────────────────────────────────

// Role define o papel de um usuário no sistema.
type Role string

const (
	RolePatient Role = "patient"
	RoleRescuer Role = "rescuer"
	RoleDoctor  Role = "doctor"
	RoleAdmin   Role = "admin"
)

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}
