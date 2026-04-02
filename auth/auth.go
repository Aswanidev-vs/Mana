package auth

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Role defines access levels for RBAC.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
	RoleGuest Role = "guest"
)

// Claims extends standard JWT claims with Mana-specific fields.
type Claims struct {
	jwt.RegisteredClaims
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     Role   `json:"role"`
}

// Config holds JWT authentication configuration.
type Config struct {
	Secret      string        `json:"secret"`
	Issuer      string        `json:"issuer"`
	TokenExpiry time.Duration `json:"token_expiry"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Secret:      "mana-secret-change-me",
		Issuer:      "mana",
		TokenExpiry: 24 * time.Hour,
	}
}

// JWTAuth handles JWT token generation and validation.
type JWTAuth struct {
	config Config
	parser *jwt.Parser
}

// NewJWTAuth creates a new JWTAuth with the given config.
func NewJWTAuth(cfg Config) *JWTAuth {
	parser := jwt.NewParser(
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithValidMethods([]string{"HS256"}),
	)
	return &JWTAuth{
		config: cfg,
		parser: parser,
	}
}

// GenerateToken creates a signed JWT for the given user.
func (a *JWTAuth) GenerateToken(userID, username string, role Role) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.config.Issuer,
			ExpiresAt: jwt.NewNumericDate(now.Add(a.config.TokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   userID,
		},
		UserID:   userID,
		Username: username,
		Role:     role,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.Secret))
}

// ValidateToken parses and validates a JWT string.
func (a *JWTAuth) ValidateToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := a.parser.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(a.config.Secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

// ExtractToken extracts a Bearer token from an Authorization header.
func ExtractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}

	return parts[1]
}

// ExtractTokenFromQuery extracts a token from a URL query parameter.
func ExtractTokenFromQuery(r *http.Request) string {
	return r.URL.Query().Get("token")
}

// Permission defines a specific action that can be authorized.
type Permission string

const (
	PermRoomCreate  Permission = "room:create"
	PermRoomJoin    Permission = "room:join"
	PermRoomDelete  Permission = "room:delete"
	PermMessageSend Permission = "message:send"
	PermCallStart   Permission = "call:start"
	PermCallEnd     Permission = "call:end"
	PermAdminAll    Permission = "admin:all"
)

// RBAC manages role-based access control.
type RBAC struct {
	mu        sync.RWMutex
	rolePerms map[Role][]Permission
}

// NewRBAC creates a new RBAC manager with default permissions.
func NewRBAC() *RBAC {
	rbac := &RBAC{
		rolePerms: map[Role][]Permission{
			RoleGuest: {
				PermRoomJoin,
			},
			RoleUser: {
				PermRoomCreate,
				PermRoomJoin,
				PermMessageSend,
				PermCallStart,
				PermCallEnd,
			},
			RoleAdmin: {
				PermRoomCreate,
				PermRoomJoin,
				PermRoomDelete,
				PermMessageSend,
				PermCallStart,
				PermCallEnd,
				PermAdminAll,
			},
		},
	}
	return rbac
}

// Authorize checks if a role has a specific permission.
func (r *RBAC) Authorize(role Role, perm Permission) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	perms, ok := r.rolePerms[role]
	if !ok {
		return false
	}

	for _, p := range perms {
		if p == perm || p == PermAdminAll {
			return true
		}
	}
	return false
}

// GrantPermission adds a permission to a role.
func (r *RBAC) GrantPermission(role Role, perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rolePerms[role] = append(r.rolePerms[role], perm)
}

// RevokePermission removes a permission from a role.
func (r *RBAC) RevokePermission(role Role, perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()

	perms := r.rolePerms[role]
	for i, p := range perms {
		if p == perm {
			r.rolePerms[role] = append(perms[:i], perms[i+1:]...)
			return
		}
	}
}
