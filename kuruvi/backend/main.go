package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/auth"
	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/product"
	"golang.org/x/crypto/bcrypt"
)

// AuthStore handles persistent user credentials with SQLite
type AuthStore struct {
	db *sql.DB
}

func NewAuthStore(db *sql.DB) (*AuthStore, error) {
	_, err := db.Exec(`
		PRAGMA journal_mode=WAL;
		CREATE TABLE IF NOT EXISTS users (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create users table: %w", err)
	}
	return &AuthStore{db: db}, nil
}

func (s *AuthStore) Register(username, password string) error {
	if username == "" || password == "" {
		return fmt.Errorf("username and password required")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = s.db.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", username, string(hashed))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("user already exists")
		}
		return err
	}
	return nil
}

func (s *AuthStore) Login(username, password string) error {
	var hashed string
	err := s.db.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&hashed)
	if err == sql.ErrNoRows {
		return fmt.Errorf("invalid credentials")
	}
	if err != nil {
		return err
	}

	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(password))
}

func main() {
	// 1. Configuration
	cfg := core.DefaultConfig()
	cfg.Port = 8080
	cfg.Host = "0.0.0.0"
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.EnableAuth = true
	cfg.JWTSecret = "kuruvi-secure-secret-key-32bytes-min"
	cfg.JWTIssuer = "kuruvi-messenger"
	cfg.AllowedOrigins = []string{"*"}
	cfg.MessageStorePath = "data/kuruvi.db"
	cfg.ProductStorePath = "data/product.json"
	cfg.AttachmentDir = "data/attachments"

	// Create data directories
	os.MkdirAll("data/attachments", 0755)

	// Initialize SQLite Database
	db, err := sql.Open("sqlite", cfg.MessageStorePath)
	if err != nil {
		log.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()

	authStore, err := NewAuthStore(db)
	if err != nil {
		log.Fatalf("Failed to initialize auth store: %v", err)
	}

	app := mana.New(cfg)
	mux := app.Mux()
	productStore := app.ProductStore()

	// Helper for CORS
	cors := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			h(w, r)
		}
	}

	// 2. Auth Implementation
	mux.HandleFunc("/api/auth/register", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[Auth] Register decode error: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		log.Printf("[Auth] Register attempt for user: %s", req.Username)
		if err := authStore.Register(req.Username, req.Password); err != nil {
			log.Printf("[Auth] Register failed for %s: %v", req.Username, err)
			status := http.StatusInternalServerError
			if err.Error() == "user already exists" {
				status = http.StatusConflict
			} else if err.Error() == "username and password required" {
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}

		_ = productStore.UpsertProfile(product.Profile{
			UserID:      req.Username,
			DisplayName: req.Username,
			Status:      "Hey there! I am using Kuruvi.",
		})

		token, err := app.JWTAuth().GenerateToken(req.Username, req.Username, auth.RoleUser)
		if err != nil {
			log.Printf("[Auth] Token generation failed: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Printf("[Auth] Register success: %s", req.Username)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"token": token, "username": req.Username})
	}))

	mux.HandleFunc("/api/auth/login", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[Auth] Login decode error: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		log.Printf("[Auth] Login attempt for user: %s", req.Username)
		if err := authStore.Login(req.Username, req.Password); err != nil {
			log.Printf("[Auth] Login failed for %s: %v", req.Username, err)
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		token, err := app.JWTAuth().GenerateToken(req.Username, req.Username, auth.RoleUser)
		if err != nil {
			log.Printf("[Auth] Token generation failed: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Printf("[Auth] Login success: %s", req.Username)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token, "username": req.Username})
	}))

	// 3. Static Files & Frontend SPA Routing
	frontendPath := "../frontend"
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		fullPath := filepath.Join(frontendPath, path)
		if strings.HasPrefix(path, "/api/") || path == "/ws" || path == "/health" {
			return // Let other handlers handle these
		}

		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			// SPA fallback
			http.ServeFile(w, r, filepath.Join(frontendPath, "index.html"))
			return
		}
		http.ServeFile(w, r, fullPath)
	})

	// 4. E2EE Public Key Exchange API
	keyExchange := app.KeyExchange()
	mux.HandleFunc("/api/e2ee/pubkey", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			http.Error(w, "user_id required", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			pub, ok := keyExchange.GetPublicKey(userID)
			if !ok {
				http.Error(w, "Key not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"user_id": userID, "public_key": pub})
		case http.MethodPost:
			var body struct {
				PublicKey []byte `json:"public_key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Invalid JSON", http.StatusBadRequest)
				return
			}
			keyExchange.StorePublicKey(userID, body.PublicKey)
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// 5. Contacts API - list online users from product store recent devices
	mux.HandleFunc("/api/contacts", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(cfg.ProductStorePath)
		if err != nil {
			http.Error(w, "store read error", http.StatusInternalServerError)
			return
		}
		var snap product.Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			http.Error(w, "store parse error", http.StatusInternalServerError)
			return
		}

		now := time.Now()
		const onlineCutoff = 5 * time.Minute
		contacts := []map[string]interface{}{}

		selfUserID := ""
		tokenStr := auth.ExtractTokenFromQuery(r)
		if tokenStr != "" && app.JWTAuth() != nil {
			claims, _ := app.JWTAuth().ValidateToken(tokenStr)
			selfUserID = claims.UserID
		}

		for userID, devices := range snap.Devices {
			if userID == selfUserID {
				continue
			}
			lastSeen := time.Time{}
			for _, d := range devices {
				if d.LastSeenAt.After(lastSeen) {
					lastSeen = d.LastSeenAt
				}
			}
			if now.Sub(lastSeen) > onlineCutoff {
				continue
			}
			p, ok := snap.Profiles[userID]
			if !ok {
				p = product.Profile{UserID: userID, DisplayName: userID}
			}
			status := "Online"
			if p.Status != "" {
				status = p.Status
			}
			contacts = append(contacts, map[string]interface{}{
				"id":      userID,
				"name":    p.DisplayName,
				"status":  status,
				"lastMsg": "Say hi to start chatting!",
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(contacts)
	})

	// 6. Presence & Message Logging
	app.OnUserJoin(func(roomID string, user core.User) {
		log.Printf("[Presence] %s (id:%s) joined room '%s'", user.Username, user.ID, roomID)
	})

	app.OnMessage(func(msg core.Message) {
		log.Printf("[Message stored] ID:%s from %s to target:%s room:%s payload len:%d", msg.ID, msg.SenderID, msg.TargetID, msg.RoomID, len(msg.Payload))
	})

	// Start Server
	log.Printf("Kuruvi backend starting on http://%s:%d", cfg.Host, cfg.Port)
	if err := app.StartWithGracefulShutdown(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}

}
