package jwt

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Типы токенов.
const (
	TokenAccess  = "access"
	TokenRefresh = "refresh"
)

type TokenPair struct {
	AccessToken  string `json:"access"`
	RefreshToken string `json:"refresh"`
}

type Claims struct {
	UserID    int64  `json:"user_id"`
	Role      string `json:"role"`
	TokenType string `json:"token_type"` // access | refresh
	jwt.RegisteredClaims
}

type Manager struct {
	secret          string
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewManager(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{
		secret:          secret,
		accessTokenTTL:  accessTTL,
		refreshTokenTTL: refreshTTL,
	}
}

// NewManagerFromVars creates a Manager from string duration values (e.g. "60m", "168h").
// Falls back to safe defaults if parsing fails.
func NewManagerFromVars(secret, accessTTLStr, refreshTTLStr string) *Manager {
	accessTTL, err := time.ParseDuration(accessTTLStr)
	if err != nil || accessTTL <= 0 {
		accessTTL = 60 * time.Minute
	}
	refreshTTL, err := time.ParseDuration(refreshTTLStr)
	if err != nil || refreshTTL <= 0 {
		refreshTTL = 7 * 24 * time.Hour
	}
	return NewManager(secret, accessTTL, refreshTTL)
}

// GenerateTokenPair creates an access + refresh token pair.
func (m *Manager) GenerateTokenPair(userID int64, role string) (*TokenPair, error) {
	access, err := m.generateToken(userID, role, TokenAccess, m.accessTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}
	refresh, err := m.generateToken(userID, role, TokenRefresh, m.refreshTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

// GenerateAccess creates a new access token (used by /api/auth/refresh/).
func (m *Manager) GenerateAccess(userID int64, role string) (string, error) {
	return m.generateToken(userID, role, TokenAccess, m.accessTokenTTL)
}

func (m *Manager) generateToken(userID int64, role, tokenType string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:    userID,
		Role:      role,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(m.secret))
}

// ParseToken проверяет подпись и срок; wantType — "access" или "refresh".
func (m *Manager) ParseToken(tokenString, wantType string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(m.secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if wantType != "" && claims.TokenType != wantType {
		return nil, fmt.Errorf("wrong token type")
	}
	return claims, nil
}
