package types

import "time"

// Роли пользователей.
//
//	admin      - администратор: видит и меняет всё
//	it_head    - руководитель IT: те же права, что у admin
//	dispatcher - диспетчер: видит всех клиентов и оборудование, только просмотр
//	user       - клиент: видит только своё оборудование, только просмотр
const (
	RoleAdmin      = "admin"
	RoleITHead     = "it_head"
	RoleDispatcher = "dispatcher"
	RoleUser       = "user"
)

var RoleDisplay = map[string]string{
	RoleAdmin:      "Администратор",
	RoleITHead:     "Руководитель IT",
	RoleDispatcher: "Диспетчер",
	RoleUser:       "Клиент",
}

func ValidRole(role string) bool {
	_, ok := RoleDisplay[role]
	return ok
}

// User — пользователь системы (сотрудник или клиент).
type User struct {
	ID           int64      `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	FirstName    string     `json:"first_name"`
	LastName     string     `json:"last_name"`
	Email        string     `json:"email"`
	Phone        string     `json:"phone"`
	Position     string     `json:"position"`
	Role         string     `json:"role"`
	IsActive     bool       `json:"is_active"`
	IsSuperuser  bool       `json:"-"`
	DateJoined   time.Time  `json:"date_joined"`
	LastLogin    *time.Time `json:"last_login"`

	// ClientID — id клиента, к которому привязан аккаунт (заполняется join'ом).
	ClientID *int64 `json:"client_id"`
}

// IsManager — может всё менять (admin или it_head).
func (u *User) IsManager() bool {
	return u != nil && (u.Role == RoleAdmin || u.Role == RoleITHead)
}

// IsStaffViewer — видит ВСЕХ клиентов (admin, it_head, dispatcher).
func (u *User) IsStaffViewer() bool {
	return u != nil && (u.Role == RoleAdmin || u.Role == RoleITHead || u.Role == RoleDispatcher)
}

// UserView — пользователь для CRUD /api/users/.
type UserView struct {
	User
	RoleDisplay string `json:"role_display"`
}

func NewUserView(u User) UserView {
	return UserView{User: u, RoleDisplay: RoleDisplay[u.Role]}
}

// UserWrite — поля для создания/изменения пользователя (PATCH: только переданные).
type UserWrite struct {
	Username  *string `json:"username"`
	Password  *string `json:"password"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	Email     *string `json:"email"`
	Phone     *string `json:"phone"`
	Position  *string `json:"position"`
	Role      *string `json:"role"`
	IsActive  *bool   `json:"is_active"`
}

// UserFilter — фильтры списка пользователей.
type UserFilter struct {
	Role   string
	Staff  bool
	Search string
}

// MeClient — краткие данные клиента для /api/me/.
type MeClient struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
}

// Me — данные текущего пользователя для /api/me/ и логина.
type Me struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	FirstName   string    `json:"first_name"`
	LastName    string    `json:"last_name"`
	Email       string    `json:"email"`
	Phone       string    `json:"phone"`
	Position    string    `json:"position"`
	Role        string    `json:"role"`
	RoleDisplay string    `json:"role_display"`
	ClientID    *int64    `json:"client_id"`
	Client      *MeClient `json:"client"`
	CanEdit     bool      `json:"can_edit"`
	CanViewAll  bool      `json:"can_view_all"`
}
