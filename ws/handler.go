package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Aswanidev-vs/mana/auth"
	"github.com/Aswanidev-vs/mana/core"
)

// HandlerConfig holds configuration for the WebSocket handler.
type HandlerConfig struct {
	EnableAuth     bool
	JWTAuth        *auth.JWTAuth
	RateLimiter    *auth.RateLimiter
	MaxMessageSize int64
	AllowedOrigins []string
	Acceptor       Acceptor
	OnConnect      func(peerID, username, userRole string, conn Conn)
	OnDisconnect   func(peerID string)
	OnMessage      func(peerID, username, userRole string, data []byte)
	Logger         interface {
		Info(string, ...interface{})
		Error(string, ...interface{})
		Warn(string, ...interface{})
		Debug(string, ...interface{})
	}
}

// Handler manages WebSocket connections including auth, rate limiting, and message reading.
type Handler struct {
	config HandlerConfig
}

// NewHandler creates a new WebSocket handler.
func NewHandler(cfg HandlerConfig) *Handler {
	if cfg.Acceptor == nil {
		cfg.Acceptor = NewCoderAcceptor()
	}
	return &Handler{config: cfg}
}

// ServeHTTP handles WebSocket upgrade requests. Implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Rate limiting by IP
	if h.config.RateLimiter != nil {
		ip := extractIP(r)
		if !h.config.RateLimiter.Allow(ip) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
	}

	// Authentication
	userID, username, userRole := h.authenticate(r)
	if h.config.EnableAuth && userID == "" {
		if h.config.Logger != nil {
			h.config.Logger.Warn("[WS] Authentication failed for remote %s", r.RemoteAddr)
		}
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	sessionID := buildSessionID(userID, r.URL.Query().Get("device_id"))
	if h.config.Logger != nil {
		h.config.Logger.Info("[WS] Authenticated user: %s (session: %s, role: %s)", userID, sessionID, userRole)
	}

	// WebSocket upgrade
	conn, err := h.config.Acceptor.Accept(w, r, AcceptConfig{
		AllowedOrigins: h.config.AllowedOrigins,
		Subprotocols:   []string{"mana"},
	})
	if err != nil {
		return
	}

	wsConn := conn

	if h.config.MaxMessageSize > 0 {
		if limited, ok := wsConn.(ReadLimitConn); ok {
			limited.SetReadLimit(h.config.MaxMessageSize)
		}
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Notify connect
	if h.config.OnConnect != nil {
		h.config.OnConnect(sessionID, username, userRole, wsConn)
	}

	// Read loop
	for {
		data, err := wsConn.Read(ctx)
		if err != nil {
			break
		}

		// Per-user rate limiting
		if h.config.RateLimiter != nil {
			if !h.config.RateLimiter.Allow("msg:" + userID) {
				errData, _ := json.Marshal(map[string]string{"type": "error", "error": "rate_limited"})
				wsConn.Write(ctx, errData)
				continue
			}
		}

		if h.config.OnMessage != nil {
			h.config.OnMessage(sessionID, username, userRole, data)
		}
	}

	// Notify disconnect
	if h.config.OnDisconnect != nil {
		h.config.OnDisconnect(sessionID)
	}
}

// authenticate extracts user identity from JWT token or query params.
func (h *Handler) authenticate(r *http.Request) (userID, username, userRole string) {
	if h.config.EnableAuth && h.config.JWTAuth != nil {
		tokenStr := auth.ExtractTokenFromQuery(r)
		if tokenStr == "" {
			tokenStr = auth.ExtractToken(r)
		}
		if tokenStr == "" {
			return "", "", ""
		}

		claims, err := h.config.JWTAuth.ValidateToken(tokenStr)
		if err != nil {
			if h.config.Logger != nil {
				h.config.Logger.Error("[WS] JWT validation error: %v", err)
			}
			return "", "", ""
		}

		return claims.UserID, claims.Username, string(claims.Role)
	}

	// No auth: extract from query
	userID = r.URL.Query().Get("user_id")
	username = r.URL.Query().Get("username")
	if userID == "" {
		userID = fmt.Sprintf("anon-%d", time.Now().UnixNano())
	}
	if username == "" {
		username = userID
	}
	return userID, username, ""
}

// extractIP gets the client IP from the request, respecting X-Forwarded-For.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func buildSessionID(userID, deviceID string) string {
	if userID == "" {
		userID = fmt.Sprintf("anon-%d", time.Now().UnixNano())
	}
	if deviceID == "" {
		return userID
	}
	// Note: We use :: twice to stay consistent with framework session splitting logic
	return userID + "::" + deviceID
}

// EncodeSignal marshals a signal to JSON bytes.
func EncodeSignal(signal core.Signal) ([]byte, error) {
	return json.Marshal(signal)
}

// DecodeSignal unmarshals a signal from JSON bytes.
func DecodeSignal(data []byte) (*core.Signal, error) {
	var signal core.Signal
	if err := json.Unmarshal(data, &signal); err != nil {
		return nil, err
	}
	return &signal, nil
}
