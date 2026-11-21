package services

import (
	"context"
	"net/http"
	"time"

	"ata-energo/db/repositories"
	jwtpkg "ata-energo/pkg/jwt"
	"ata-energo/types"

	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	users   repositories.UserRepository
	clients repositories.ClientRepository
	jwt     *jwtpkg.Manager
}

func NewAuthService(users repositories.UserRepository, clients repositories.ClientRepository, jwt *jwtpkg.Manager) *AuthService {
	return &AuthService{users: users, clients: clients, jwt: jwt}
}

// HashPassword — bcrypt-хеш пароля.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

// LoginResult — ответ POST /api/auth/login/.
type LoginResult struct {
	Access  string   `json:"access"`
	Refresh string   `json:"refresh"`
	User    types.Me `json:"user"`
}

// Login проверяет логин/пароль и возвращает токены + данные пользователя.
func (s *AuthService) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	u, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.IsActive || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, Detail(http.StatusUnauthorized, "Не найдено активной учётной записи с указанными данными")
	}
	pair, err := s.jwt.GenerateTokenPair(u.ID, u.Role)
	if err != nil {
		return nil, err
	}
	_ = s.users.SetLastLogin(ctx, u.ID, time.Now().UTC())
	me, err := s.Me(ctx, u)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Access: pair.AccessToken, Refresh: pair.RefreshToken, User: *me}, nil
}

// Refresh выдаёт новый access-токен по refresh-токену.
func (s *AuthService) Refresh(ctx context.Context, refresh string) (string, error) {
	claims, err := s.jwt.ParseToken(refresh, jwtpkg.TokenRefresh)
	if err != nil {
		return "", Detail(http.StatusUnauthorized, "Токен недействителен или просрочен")
	}
	u, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		return "", err
	}
	if u == nil || !u.IsActive {
		return "", Detail(http.StatusUnauthorized, "Пользователь не найден или заблокирован")
	}
	return s.jwt.GenerateAccess(u.ID, u.Role)
}

// UserFromAccess — пользователь по access-токену (для REST и WebSocket).
func (s *AuthService) UserFromAccess(ctx context.Context, token string) (*types.User, error) {
	claims, err := s.jwt.ParseToken(token, jwtpkg.TokenAccess)
	if err != nil {
		return nil, nil
	}
	u, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.IsActive {
		return nil, nil
	}
	return u, nil
}

// Me — данные текущего пользователя (/api/me/).
func (s *AuthService) Me(ctx context.Context, u *types.User) (*types.Me, error) {
	me := &types.Me{
		ID: u.ID, Username: u.Username, FirstName: u.FirstName, LastName: u.LastName, Email: u.Email,
		Phone: u.Phone, Position: u.Position, Role: u.Role, RoleDisplay: types.RoleDisplay[u.Role],
		ClientID: u.ClientID, CanEdit: u.IsManager(), CanViewAll: u.IsStaffViewer(),
	}
	if u.ClientID != nil {
		c, err := s.clients.GetByID(ctx, *u.ClientID)
		if err != nil {
			return nil, err
		}
		if c != nil {
			me.Client = &types.MeClient{ID: c.ID, Name: c.Name, Address: c.Address, Phone: c.Phone}
		}
	}
	return me, nil
}
