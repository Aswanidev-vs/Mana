package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/auth"
	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/product"
	"github.com/Aswanidev-vs/mana/storage/db"
)

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
	cfg.ProductStorePath = "data/product.json"
	cfg.AttachmentDir = "data/attachments"

	// Plug-and-play database: point the framework at our SQLite file.
	// The framework will auto-create all tables (accounts, messages, profiles, etc.)
	cfg.DatabaseDriver = db.SQLite
	cfg.DatabaseDSN = "data/kuruvi.db"

	// Create data directories
	os.MkdirAll("data/attachments", 0755)
	os.MkdirAll("data", 0755)

	// 2. Spin up Mana — DatabaseDSN causes the framework to initialize
	//    AccountStore, MessageStore, ProfileStore, ContactStore automatically.
	app := mana.New(cfg)
	mux := app.Mux()
	productStore := app.ProductStore()

	// Verify stores were wired up properly
	if app.AccountStore() == nil {
		log.Fatal("AccountStore is nil — check DatabaseDSN config")
	}
	if app.MessageStore() == nil {
		log.Fatal("MessageStore is nil — check DatabaseDSN config")
	}

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

	// 3. Auth — Register
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
		if req.Username == "" || req.Password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}

		log.Printf("[Auth] Register attempt for user: %s", req.Username)

		ctx := r.Context()
		if err := app.AccountStore().CreateUser(ctx, req.Username, req.Password); err != nil {
			log.Printf("[Auth] Register failed for %s: %v", req.Username, err)
			status := http.StatusInternalServerError
			errMsg := err.Error()
			// Detect duplicate key across SQLite / Postgres / MySQL
			if strings.Contains(errMsg, "UNIQUE") || strings.Contains(errMsg, "unique") ||
				strings.Contains(errMsg, "duplicate") || strings.Contains(errMsg, "Duplicate") {
				status = http.StatusConflict
				errMsg = "user already exists"
			}
			http.Error(w, errMsg, status)
			return
		}

		// The framework's SQLAccountStore prefixes user IDs with "u-"
		userID := fmt.Sprintf("u-%s", req.Username)

		// Upsert profile in both stores (framework profile + product store)
		if app.ProfileStore() != nil {
			_ = app.ProfileStore().UpsertProfile(ctx, core.UserProfile{
				UserID:      userID,
				DisplayName: req.Username,
				UpdatedAt:   time.Now(),
			})
		}
		if productStore != nil {
			_ = productStore.UpsertProfile(product.Profile{
				UserID:      req.Username,
				DisplayName: req.Username,
				Status:      "Hey there! I am using Kuruvi.",
			})
		}

		token, err := app.JWTAuth().GenerateToken(userID, req.Username, auth.RoleUser)
		if err != nil {
			log.Printf("[Auth] Token generation failed: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Printf("[Auth] Register success: %s (id: %s)", req.Username, userID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"token": token, "username": req.Username})
	}))

	// 4. Auth — Login
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

		ctx := r.Context()
		userID, err := app.AccountStore().Authenticate(ctx, req.Username, req.Password)
		if err != nil {
			log.Printf("[Auth] Login failed for %s: %v", req.Username, err)
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		token, err := app.JWTAuth().GenerateToken(userID, req.Username, auth.RoleUser)
		if err != nil {
			log.Printf("[Auth] Token generation failed: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Printf("[Auth] Login success: %s (id: %s)", req.Username, userID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token, "username": req.Username})
	}))

	// 5. Static Files & Frontend SPA Routing
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

	// 6. E2EE Public Key Exchange API
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

	// 7. Contacts API — list recently active users from the product store
	mux.HandleFunc("/api/contacts", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(cfg.ProductStorePath)
		if err != nil {
			// Product store hasn't been written yet — return empty list
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]interface{}{})
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
			if claims != nil {
				selfUserID = claims.UserID
			}
		}

		for userID, devices := range snap.Devices {
			// Compare against both the raw username and the prefixed userID
			if userID == selfUserID || "u-"+userID == selfUserID {
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

	// 8. Presence & Message Logging hooks
	app.OnUserJoin(func(roomID string, user core.User) {
		log.Printf("[Presence] %s (id:%s) joined room '%s'", user.Username, user.ID, roomID)
	})

	app.OnMessage(func(msg core.Message) {
		log.Printf("[Message stored] ID:%s from %s to target:%s room:%s payload len:%d",
			msg.ID, msg.SenderID, msg.TargetID, msg.RoomID, len(msg.Payload))
	})

	// 9. Start Server
	log.Printf("Kuruvi backend starting on http://%s:%d", cfg.Host, cfg.Port)
	log.Printf("Database: SQLite @ %s (driver managed by Mana framework)", cfg.DatabaseDSN)
	if err := app.StartWithGracefulShutdown(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// resolveUserContext extracts token claims from authorization header or query param.
func resolveUserContext(r *http.Request, jwtAuth *auth.JWTAuth) (userID, username string) {
	if jwtAuth == nil {
		return "", ""
	}
	tokenStr := auth.ExtractTokenFromQuery(r)
	if tokenStr == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	if tokenStr == "" {
		return "", ""
	}
	claims, err := jwtAuth.ValidateToken(tokenStr)
	if err != nil || claims == nil {
		return "", ""
	}
	return claims.UserID, claims.Username
}

// Ensure context is used (resolveUserContext is a helper available for future endpoints)
var _ = context.Background
