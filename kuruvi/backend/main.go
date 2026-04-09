package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/wneessen/go-mail"
	"golang.org/x/crypto/bcrypt"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/auth"
	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/product"
	"github.com/Aswanidev-vs/mana/storage/db"
	_ "modernc.org/sqlite"
)

func main() {
	// Load .env explicitly for Kuruvi configurations (like SMTP)
	_ = godotenv.Load()

	// 1. Configuration
	cfg := core.DefaultConfig()
	cfg.Port = 8080
	cfg.Host = "localhost"
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.EnableAuth = true
	cfg.JWTSecret = "kuruvi-secure-secret-key-32bytes-min"
	cfg.JWTIssuer = "kuruvi-messenger"
	cfg.AllowedOrigins = []string{"*"}
	// cfg.ProductStorePath = "data/product.json" // Commented out to avoid nil product store panic
	cfg.AttachmentDir = "data/attachments"

	// Plug-and-play database: point the framework at our SQLite file.
	// The framework will auto-create all tables (accounts, messages, profiles, etc.)
	cfg.DatabaseDriver = db.SQLite
	cfg.DatabaseDSN = "data/kuruvi.db"

	// Create data directories
	os.MkdirAll("data/attachments", 0755)
	os.MkdirAll("data", 0755)

	// Ensure product store file exists with proper structure to avoid nil pointer in framework
	if _, err := os.Stat(cfg.ProductStorePath); os.IsNotExist(err) {
		os.WriteFile(cfg.ProductStorePath, []byte(`{"Devices":{},"Profiles":{}}`), 0644)
	}

	// 2. Spin up Mana — DatabaseDSN causes the framework to initialize
	//    AccountStore, MessageStore, ProfileStore, ContactStore automatically.
	app := mana.New(cfg)
	mux := app.Mux()
	productStore := app.ProductStore()

	// 2b. Run Kuruvi Local Migrations AFTER framework creates base tables
	migrationDB, err := sql.Open("sqlite", cfg.DatabaseDSN)
	if err != nil {
		log.Printf("Migration db open error: %v", err)
	} else {
		// Kuruvi Local Specialized Tables
		kuruviTables := `
			CREATE TABLE IF NOT EXISTS kuruvi_profiles (
				user_id TEXT PRIMARY KEY,
				kuruvi_id TEXT UNIQUE,
				last_username_update DATETIME,
				phone TEXT
			);
			CREATE TABLE IF NOT EXISTS kuruvi_contacts (
				user_id TEXT,
				contact_id TEXT,
				last_msg TEXT,
				updated_at DATETIME,
				PRIMARY KEY (user_id, contact_id)
			);
			CREATE TABLE IF NOT EXISTS kuruvi_groups (
				room_id TEXT PRIMARY KEY,
				name TEXT,
				creator_id TEXT,
				created_at DATETIME
			);
			CREATE TABLE IF NOT EXISTS kuruvi_group_members (
				room_id TEXT,
				user_id TEXT,
				role TEXT, -- 'admin' or 'member'
				joined_at DATETIME,
				PRIMARY KEY (room_id, user_id)
			);
		`
		if _, err := migrationDB.Exec(kuruviTables); err != nil {
			log.Printf("Failed to setup Kuruvi local tables: %v", err)
		}

		queries := []string{
			`ALTER TABLE accounts ADD COLUMN phone TEXT`,
			`ALTER TABLE accounts ADD COLUMN email TEXT`,
		}
		for _, q := range queries {
			_, err = migrationDB.Exec(q)
			if err != nil && !strings.Contains(err.Error(), "duplicate column name") && !strings.Contains(err.Error(), "no such table") {
				log.Printf("Migration error for %s: %v", q, err)
			}
		}
		migrationDB.Close()
	}

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
			Phone    string `json:"phone,omitempty"`
			Email    string `json:"email,omitempty"`
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
		if req.Phone == "" && req.Email == "" {
			http.Error(w, "either phone or email is required for registration", http.StatusBadRequest)
			return
		}

		log.Printf("[Auth] Register attempt for user: %s (phone: %s, email: %s)", req.Username, req.Phone, req.Email)

		ctx := r.Context()

		// 1. Framework Identity Hand-off
		// Call the underlying base authentication mechanism.
		type ContactCreator interface {
			CreateUserWithContact(ctx context.Context, username, password, phone, email string) error
		}
		if contactStore, ok := app.AccountStore().(ContactCreator); ok && (req.Phone != "" || req.Email != "") {
			if err := contactStore.CreateUserWithContact(ctx, req.Username, req.Password, req.Phone, req.Email); err != nil {
				status := http.StatusInternalServerError
				errMsg := err.Error()
				if strings.Contains(errMsg, "UNIQUE") || strings.Contains(errMsg, "duplicate") {
					status = http.StatusConflict
					errMsg = "username already exists"
				}
				http.Error(w, errMsg, status)
				return
			}
		} else {
			http.Error(w, "account store configuration error", http.StatusInternalServerError)
			return
		}

		// 2. Retrieve Framework deterministic ID
		frameworkUserID, err := app.AccountStore().Authenticate(ctx, req.Username, req.Password)
		if err != nil {
			http.Error(w, "Internal server error fetching ID", http.StatusInternalServerError)
			return
		}

		// 3. Kuruvi Local Specialized Domain ID Generation
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		kuruviID := fmt.Sprintf("%s#%04d", req.Username, rng.Intn(10000))

		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err == nil {
			defer dbConn.Close()
			_, err = dbConn.Exec("INSERT INTO kuruvi_profiles (user_id, kuruvi_id, phone, last_username_update) VALUES (?, ?, ?, ?)",
				frameworkUserID, kuruviID, req.Phone, time.Now().Add(-24*time.Hour))
			if err != nil {
				log.Printf("Failed to map kuruvi ID: %v", err)
			}
		}

		// Actual Email Delivery via go-mail (dispatched asynchronously)
		if req.Email != "" {
			go sendWelcomeEmail(req.Email, kuruviID)
		} else {
			log.Printf("[Auth] No email provided, skipping welcome mail for %s", kuruviID)
		}

		// Upsert profile in both stores (framework profile + product store)
		if app.ProfileStore() != nil {
			_ = app.ProfileStore().UpsertProfile(ctx, core.UserProfile{
				UserID:      frameworkUserID,
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

		token, err := app.JWTAuth().GenerateToken(frameworkUserID, req.Username, auth.RoleUser)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"token":     token,
			"username":  req.Username,
			"user_id":   frameworkUserID,
			"unique_id": kuruviID,
		})
	}))

	// 4. Auth — Login (Username)
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

		// 1. Open a local DB connection to retrieve Kuruvi-specific metadata
		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			log.Printf("[Auth] Login db open error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		// 2. Retrieve Kuruvi Unique ID
		var kuruviID string
		_ = dbConn.QueryRowContext(ctx, "SELECT kuruvi_id FROM kuruvi_profiles WHERE user_id = ?", userID).Scan(&kuruviID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":     token,
			"username":  req.Username,
			"user_id":   userID,
			"unique_id": kuruviID,
		})
	}))

	// 4b. Auth — Login by Phone Number
	mux.HandleFunc("/api/auth/login/phone", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Phone    string `json:"phone"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[Auth] Phone login decode error: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Phone == "" {
			http.Error(w, "phone number required", http.StatusBadRequest)
			return
		}

		log.Printf("[Auth] Phone login attempt for: %s", req.Phone)

		ctx := r.Context()

		// Type assert to access extended methods
		type PhoneAuthenticator interface {
			AuthenticateByPhone(ctx context.Context, phone, password string) (string, error)
		}
		phoneStore, ok := app.AccountStore().(PhoneAuthenticator)
		if !ok {
			log.Printf("[Auth] Phone authentication not supported by account store")
			http.Error(w, "Phone login not supported", http.StatusInternalServerError)
			return
		}

		userID, err := phoneStore.AuthenticateByPhone(ctx, req.Phone, req.Password)
		if err != nil {
			log.Printf("[Auth] Phone login failed for %s: %v", req.Phone, err)
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Get username for token
		user, err := app.AccountStore().GetUser(ctx, userID)
		if err != nil {
			log.Printf("[Auth] Failed to get user after phone login: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		token, err := app.JWTAuth().GenerateToken(userID, user.Username, auth.RoleUser)
		if err != nil {
			log.Printf("[Auth] Token generation failed: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Printf("[Auth] Phone login success: %s (id: %s)", req.Phone, userID)

		// 1. Open a local DB connection to retrieve Kuruvi-specific metadata
		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			log.Printf("[Auth] Phone login db open error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		// 2. Retrieve Kuruvi Unique ID
		var kuruviID string
		_ = dbConn.QueryRowContext(ctx, "SELECT kuruvi_id FROM kuruvi_profiles WHERE user_id = ?", userID).Scan(&kuruviID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":     token,
			"username":  user.Username,
			"user_id":   userID,
			"unique_id": kuruviID,
		})
	}))

	// 4c. Auth — Login by Email
	mux.HandleFunc("/api/auth/login/email", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[Auth] Email login decode error: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Email == "" {
			http.Error(w, "email required", http.StatusBadRequest)
			return
		}

		log.Printf("[Auth] Email login attempt for: %s", req.Email)

		ctx := r.Context()

		// Type assert to access extended methods
		type EmailAuthenticator interface {
			AuthenticateByEmail(ctx context.Context, email, password string) (string, error)
		}
		emailStore, ok := app.AccountStore().(EmailAuthenticator)
		if !ok {
			log.Printf("[Auth] Email authentication not supported by account store")
			http.Error(w, "Email login not supported", http.StatusInternalServerError)
			return
		}

		userID, err := emailStore.AuthenticateByEmail(ctx, req.Email, req.Password)
		if err != nil {
			log.Printf("[Auth] Email login failed for %s: %v", req.Email, err)
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Get username for token
		user, err := app.AccountStore().GetUser(ctx, userID)
		if err != nil {
			log.Printf("[Auth] Failed to get user after email login: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		token, err := app.JWTAuth().GenerateToken(userID, user.Username, auth.RoleUser)
		if err != nil {
			log.Printf("[Auth] Token generation failed: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Printf("[Auth] Email login success: %s (id: %s)", req.Email, userID)

		// 1. Open a local DB connection to retrieve Kuruvi-specific metadata
		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			log.Printf("[Auth] Email login db open error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		// 2. Retrieve Kuruvi Unique ID
		var kuruviID string
		_ = dbConn.QueryRowContext(ctx, "SELECT kuruvi_id FROM kuruvi_profiles WHERE user_id = ?", userID).Scan(&kuruviID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":     token,
			"username":  user.Username,
			"user_id":   userID,
			"unique_id": kuruviID,
		})
	}))

	// 4d. Auth — Reset Password
	mux.HandleFunc("/api/auth/reset-password", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Identifier  string `json:"identifier"`
			NewPassword string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if req.Identifier == "" || req.NewPassword == "" {
			http.Error(w, "Identifier and new password required", http.StatusBadRequest)
			return
		}

		// Directly update the DB since AccountStore doesn't expose a SetPassword interface yet
		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			log.Printf("[Auth] Reset Password db open error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		// Manually hash password using bcrypt to maintain framework compatibility
		hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		// Try to match identifier against username, email, phone, or kuruvi ID
		queryStr := `
			UPDATE accounts 
			SET password_hash = ? 
			WHERE username = ?
			   OR email = ?
			   OR phone = ?
			   OR user_id IN (
			       SELECT user_id FROM kuruvi_profiles WHERE kuruvi_id = ? OR phone = ?
			   )
		`
		res, err := dbConn.ExecContext(ctx, queryStr, string(hashedBytes), req.Identifier, req.Identifier, req.Identifier, req.Identifier, req.Identifier)
		if err != nil {
			log.Printf("[Auth] Password reset exec error: %v", err)
			http.Error(w, "Update failed", http.StatusInternalServerError)
			return
		}

		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			http.Error(w, "User not found with provided identifier", http.StatusNotFound)
			return
		}

		log.Printf("[Auth] Password reset successful for identifier: %s", req.Identifier)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "password updated successfully"})
	}))

	// Search users by email or phone
	mux.HandleFunc("/api/search", cors(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "query required", http.StatusBadRequest)
			return
		}

		// Open db to query accounts
		db, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			log.Printf("Search db open error: %v", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer db.Close()

		// Perform Kuruvi domain-specific JOIN search connecting the underlying framework ID
		queryStr := `
			SELECT a.user_id, a.username 
			FROM accounts a 
			LEFT JOIN kuruvi_profiles kp ON a.user_id = kp.user_id
			WHERE kp.kuruvi_id = ? OR a.phone = ? OR kp.phone = ?
		`
		rows, err := db.Query(queryStr, query, query, query)
		if err != nil {
			log.Printf("Search query error: %v", err)
			http.Error(w, "query error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var results []map[string]string
		for rows.Next() {
			var userID, username string
			if err := rows.Scan(&userID, &username); err != nil {
				continue
			}
			results = append(results, map[string]string{"id": userID, "name": username})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}))

	// 4d. Auth — Update Username
	mux.HandleFunc("/api/auth/username", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID, _ := resolveUserContext(r, app.JWTAuth())
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var req struct {
			NewUsername string `json:"new_username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.NewUsername == "" {
			http.Error(w, "new_username required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			http.Error(w, "Database unavailable", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		// Kuruvi 2-Minute Constraint Local Check
		var lastUpdate sql.NullTime
		_ = dbConn.QueryRowContext(ctx, "SELECT last_username_update FROM kuruvi_profiles WHERE user_id = ?", userID).Scan(&lastUpdate)
		if lastUpdate.Valid && time.Since(lastUpdate.Time) < 2*time.Minute {
			http.Error(w, "username can only be updated every 2 minutes", http.StatusTooManyRequests)
			return
		}

		// Mutate Framework Accounts table locally directly (Kuruvi Extension)
		_, err = dbConn.ExecContext(ctx, "UPDATE accounts SET username = ? WHERE user_id = ?", req.NewUsername, userID)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				http.Error(w, "username already taken", http.StatusConflict)
			} else {
				http.Error(w, "Update failed", http.StatusInternalServerError)
			}
			return
		}

		// Set new update boundary lock
		_, _ = dbConn.ExecContext(ctx, "UPDATE kuruvi_profiles SET last_username_update = ? WHERE user_id = ?", time.Now(), userID)

		// Sync to product store profiles
		if productStore != nil {
			_ = productStore.UpsertProfile(product.Profile{
				UserID:      req.NewUsername,
				DisplayName: req.NewUsername,
			})
		}
		if app.ProfileStore() != nil {
			// Replace display name cleanly in framework memory layer
			_ = app.ProfileStore().UpsertProfile(ctx, core.UserProfile{
				UserID:      userID,
				DisplayName: req.NewUsername,
				UpdatedAt:   time.Now(),
			})
		}

		log.Printf("[Auth] Username updated successfully for %s to %s", userID, req.NewUsername)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "username updated", "username": req.NewUsername})
	}))

	// 4e. Create Group
	mux.HandleFunc("/api/group/create", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID, _ := resolveUserContext(r, app.JWTAuth())
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var req struct {
			GroupName string   `json:"group_name"`
			Members   []string `json:"members"` // array of contact UserIDs
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Generate ID
		roomID := "group-" + fmt.Sprintf("%d", time.Now().UnixNano())
		// Mana Room Manager
		newRoom := app.RoomManager().Create(roomID, req.GroupName, "group", userID)

		// Persist Kuruvi Group Metadata
		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err == nil {
			defer dbConn.Close()
			now := time.Now()
			_, _ = dbConn.Exec("INSERT INTO kuruvi_groups (room_id, name, creator_id, created_at) VALUES (?, ?, ?, ?)",
				roomID, req.GroupName, userID, now)
			
			// Add creator as Admin
			_, _ = dbConn.Exec("INSERT INTO kuruvi_group_members (room_id, user_id, role, joined_at) VALUES (?, ?, ?, ?)",
				roomID, userID, "admin", now)

			// Add initial members
			for _, mid := range req.Members {
				if mid != "" && mid != userID {
					_, _ = dbConn.Exec("INSERT OR IGNORE INTO kuruvi_group_members (room_id, user_id, role, joined_at) VALUES (?, ?, ?, ?)",
						roomID, mid, "member", now)
				}
			}
		}

		// If product store is hooked
		if app.ProductStore() != nil {
			participants := append(req.Members, userID)
			_ = app.ProductStore().UpsertConversation(product.Conversation{
				ID:           newRoom.ID(),
				IsGroup:      true,
				Title:        req.GroupName,
				Participants: participants,
			})
		}

		log.Printf("[Group] User %s created group %s (ID: %s)", userID, req.GroupName, newRoom.ID())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"room_id": newRoom.ID(), "group_name": req.GroupName})
	}))

	// 4f. Group Management Endpoints
	mux.HandleFunc("/api/group/members", cors(func(w http.ResponseWriter, r *http.Request) {
		roomID := r.URL.Query().Get("room_id")
		if roomID == "" {
			http.Error(w, "room_id required", http.StatusBadRequest)
			return
		}

		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		switch r.Method {
		case http.MethodGet:
			// List members with usernames from accounts table
			rows, err := dbConn.Query(`
				SELECT gm.user_id, a.username, gm.role, gm.joined_at
				FROM kuruvi_group_members gm
				JOIN accounts a ON gm.user_id = a.user_id
				WHERE gm.room_id = ?
			`, roomID)
			if err != nil {
				http.Error(w, "Query error", http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			var members []map[string]interface{}
			for rows.Next() {
				var uid, name, role string
				var joined time.Time
				if err := rows.Scan(&uid, &name, &role, &joined); err == nil {
					members = append(members, map[string]interface{}{
						"user_id": uid, "username": name, "role": role, "joined_at": joined,
					})
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(members)

		case http.MethodPost: // Add member
			selfID, _ := resolveUserContext(r, app.JWTAuth())
			if selfID == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if self is Admin
			var role string
			_ = dbConn.QueryRow("SELECT role FROM kuruvi_group_members WHERE room_id = ? AND user_id = ?", roomID, selfID).Scan(&role)
			if role != "admin" {
				http.Error(w, "Forbidden: Only admins can add members", http.StatusForbidden)
				return
			}

			var req struct {
				UserID string `json:"user_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid JSON", http.StatusBadRequest)
				return
			}

			_, err = dbConn.Exec("INSERT OR IGNORE INTO kuruvi_group_members (room_id, user_id, role, joined_at) VALUES (?, ?, ?, ?)",
				roomID, req.UserID, "member", time.Now())
			if err != nil {
				http.Error(w, "Failed to add member", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)

		case http.MethodDelete: // Remove member
			selfID, _ := resolveUserContext(r, app.JWTAuth())
			if selfID == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if self is Admin
			var role string
			_ = dbConn.QueryRow("SELECT role FROM kuruvi_group_members WHERE room_id = ? AND user_id = ?", roomID, selfID).Scan(&role)
			if role != "admin" {
				http.Error(w, "Forbidden: Only admins can remove members", http.StatusForbidden)
				return
			}

			userID := r.URL.Query().Get("user_id")
			if userID == "" {
				http.Error(w, "user_id required", http.StatusBadRequest)
				return
			}

			_, err = dbConn.Exec("DELETE FROM kuruvi_group_members WHERE room_id = ? AND user_id = ?", roomID, userID)
			if err != nil {
				http.Error(w, "Failed to remove member", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// 4g. Delete Group (admin only)
	mux.HandleFunc("/api/group/delete", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		selfID, _ := resolveUserContext(r, app.JWTAuth())
		if selfID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		roomID := r.URL.Query().Get("room_id")
		if roomID == "" {
			http.Error(w, "room_id required", http.StatusBadRequest)
			return
		}

		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		// Verify caller is admin
		var role string
		_ = dbConn.QueryRow("SELECT role FROM kuruvi_group_members WHERE room_id = ? AND user_id = ?", roomID, selfID).Scan(&role)
		if role != "admin" {
			http.Error(w, "Forbidden: Only admins can delete groups", http.StatusForbidden)
			return
		}

		// Delete all members, then the group
		_, _ = dbConn.Exec("DELETE FROM kuruvi_group_members WHERE room_id = ?", roomID)
		_, _ = dbConn.Exec("DELETE FROM kuruvi_groups WHERE room_id = ?", roomID)

		log.Printf("[Group] Admin %s deleted group %s", selfID, roomID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	}))

	// 4h. Leave Group (any member)
	mux.HandleFunc("/api/group/leave", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		selfID, _ := resolveUserContext(r, app.JWTAuth())
		if selfID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		roomID := r.URL.Query().Get("room_id")
		if roomID == "" {
			http.Error(w, "room_id required", http.StatusBadRequest)
			return
		}

		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		// Check if the user is the only admin — if so, they can't leave without deleting
		var role string
		_ = dbConn.QueryRow("SELECT role FROM kuruvi_group_members WHERE room_id = ? AND user_id = ?", roomID, selfID).Scan(&role)
		if role == "admin" {
			var adminCount int
			_ = dbConn.QueryRow("SELECT COUNT(*) FROM kuruvi_group_members WHERE room_id = ? AND role = 'admin'", roomID).Scan(&adminCount)
			if adminCount <= 1 {
				http.Error(w, "You are the only admin. Transfer admin role or delete the group instead.", http.StatusConflict)
				return
			}
		}

		_, err = dbConn.Exec("DELETE FROM kuruvi_group_members WHERE room_id = ? AND user_id = ?", roomID, selfID)
		if err != nil {
			http.Error(w, "Failed to leave group", http.StatusInternalServerError)
			return
		}

		log.Printf("[Group] User %s left group %s", selfID, roomID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "left"})
	}))

	// 5. Static Files & Frontend SPA Routing
	frontendPath := "../frontend"
	frontendAbs, err := filepath.Abs(frontendPath)
	if err != nil {
		log.Fatalf("failed to resolve frontend path: %v", err)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		if strings.HasPrefix(path, "/api/") || path == "/ws" || path == "/health" {
			return // Let other handlers handle these
		}

		relPath := strings.TrimPrefix(path, "/")
		fullPath := filepath.Join(frontendAbs, relPath)
		fullPathAbs, err := filepath.Abs(fullPath)
		if err != nil {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
		if fullPathAbs != frontendAbs && !strings.HasPrefix(fullPathAbs, frontendAbs+string(os.PathSeparator)) {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		if _, err := os.Stat(fullPathAbs); os.IsNotExist(err) {
			// SPA fallback
			http.ServeFile(w, r, filepath.Join(frontendAbs, "index.html"))
			return
		}
		http.ServeFile(w, r, fullPathAbs)
	})

	// 6. E2EE Public Key Exchange API
	e2eeStore := app.E2EEStore()
	mux.HandleFunc("/api/e2ee/pubkey", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			http.Error(w, "user_id required", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			if e2eeStore == nil {
				http.Error(w, "E2EE not enabled", http.StatusServiceUnavailable)
				return
			}
			pub, err := e2eeStore.LoadIdentityPublicKey(r.Context(), userID)
			if err != nil || pub == nil {
				http.Error(w, "Key not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"user_id": userID, "public_key": pub})
		case http.MethodPost:
			if e2eeStore == nil {
				http.Error(w, "E2EE not enabled", http.StatusServiceUnavailable)
				return
			}
			var body struct {
				PublicKey []byte `json:"public_key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Invalid JSON", http.StatusBadRequest)
				return
			}
			if err := e2eeStore.SaveIdentityPublicKey(r.Context(), userID, body.PublicKey); err != nil {
				log.Printf("[E2EE] Failed to save identity public key for %s: %v", userID, err)
				http.Error(w, "Failed to save key", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// 7. Contacts API — list contacts AND groups the user belongs to
	mux.HandleFunc("/api/contacts", cors(func(w http.ResponseWriter, r *http.Request) {
		// Read product store snapshot for online status (optional — may not exist)
		var snap product.Snapshot
		data, snapErr := os.ReadFile(cfg.ProductStorePath)
		if snapErr == nil {
			_ = json.Unmarshal(data, &snap)
		}
		if snap.Devices == nil {
			snap.Devices = make(map[string]map[string]product.Device)
		}
		if snap.Profiles == nil {
			snap.Profiles = make(map[string]product.Profile)
		}

		now := time.Now()
		const onlineCutoff = 5 * time.Minute

		// Resolve user from token (header OR query)
		selfUserID, _ := resolveUserContext(r, app.JWTAuth())
		if selfUserID == "" {
			// Fallback to query param for backward compat
			tokenStr := auth.ExtractTokenFromQuery(r)
			if tokenStr != "" && app.JWTAuth() != nil {
				claims, _ := app.JWTAuth().ValidateToken(tokenStr)
				if claims != nil {
					selfUserID = claims.UserID
				}
			}
		}
		if selfUserID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			http.Error(w, "Database unavailable", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		var contactsList []map[string]interface{}

		// ── 1. Direct contacts ──
		contactRows, err := dbConn.Query(`
			SELECT kc.contact_id, a.username, kc.last_msg, kc.updated_at
			FROM kuruvi_contacts kc
			JOIN accounts a ON kc.contact_id = a.user_id
			WHERE kc.user_id = ?
			ORDER BY kc.updated_at DESC
		`, selfUserID)
		if err == nil {
			for contactRows.Next() {
				var contactID, username, lastMsg string
				var updatedAt time.Time
				if err := contactRows.Scan(&contactID, &username, &lastMsg, &updatedAt); err != nil {
					continue
				}
				status := "Offline"
				if devices, ok := snap.Devices[contactID]; ok {
					var lastSeen time.Time
					for _, d := range devices {
						if d.LastSeenAt.After(lastSeen) {
							lastSeen = d.LastSeenAt
						}
					}
					if now.Sub(lastSeen) <= onlineCutoff {
						status = "Online"
					}
				}
				contactsList = append(contactsList, map[string]interface{}{
					"id": contactID, "name": username, "status": status, "lastMsg": lastMsg,
				})
			}
			contactRows.Close()
		}

		// ── 2. Groups the user belongs to ──
		groupRows, err := dbConn.Query(`
			SELECT g.room_id, g.name, g.created_at
			FROM kuruvi_group_members gm
			JOIN kuruvi_groups g ON gm.room_id = g.room_id
			WHERE gm.user_id = ?
			ORDER BY g.created_at DESC
		`, selfUserID)
		if err == nil {
			for groupRows.Next() {
				var roomID, groupName string
				var createdAt time.Time
				if err := groupRows.Scan(&roomID, &groupName, &createdAt); err != nil {
					continue
				}
				contactsList = append(contactsList, map[string]interface{}{
					"id": roomID, "name": groupName, "status": "Group", "lastMsg": "Group chat",
				})
			}
			groupRows.Close()
		}

		// ── 3. Discovery fallback: include currently online users if no contacts ──
		if len(contactsList) == 0 {
			for userID, devices := range snap.Devices {
				if userID == selfUserID {
					continue
				}
				var lastSeen time.Time
				for _, d := range devices {
					if d.LastSeenAt.After(lastSeen) {
						lastSeen = d.LastSeenAt
					}
				}
				if now.Sub(lastSeen) <= onlineCutoff {
					p := snap.Profiles[userID]
					name := userID
					if p.DisplayName != "" {
						name = p.DisplayName
					}
					contactsList = append(contactsList, map[string]interface{}{
						"id": userID, "name": name, "status": "Online", "lastMsg": "Say hi!",
					})
				}
			}
		}

		if contactsList == nil {
			contactsList = []map[string]interface{}{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(contactsList)
	}))

	// 8. Presence & Message Logging hooks
	app.OnUserJoin(func(roomID string, user core.User) {
		log.Printf("[Presence] %s (id:%s) joined room '%s'", user.Username, user.ID, roomID)
	})

	app.OnMessage(func(msg core.Message) {
		log.Printf("[Message stored] ID:%s from %s to target:%s room:%s payload len:%d",
			msg.ID, msg.SenderID, msg.TargetID, msg.RoomID, len(msg.Payload))

		// Persist interaction in kuruvi_contacts for both parties!
		// Direct to-user messaging implies TargetID is a userID.
		if msg.TargetID != "" && !strings.HasPrefix(msg.TargetID, "group-") {
			dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
			if err == nil {
				defer dbConn.Close()
				q := `
					INSERT INTO kuruvi_contacts (user_id, contact_id, last_msg, updated_at) 
					VALUES (?, ?, ?, ?)
					ON CONFLICT(user_id, contact_id) DO UPDATE SET 
						last_msg = excluded.last_msg, 
						updated_at = excluded.updated_at
				`
				// Record for Sender
				_, _ = dbConn.Exec(q, msg.SenderID, msg.TargetID, "[Encrypted]", time.Now())
				// Record for Receiver
				_, _ = dbConn.Exec(q, msg.TargetID, msg.SenderID, "[Encrypted]", time.Now())
			}
		}
	})

	// 8.1 History API — fetch messages between user and contact
	mux.HandleFunc("/api/messages/history", cors(func(w http.ResponseWriter, r *http.Request) {
		selfID, _ := resolveUserContext(r, app.JWTAuth())
		if selfID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		contactID := r.URL.Query().Get("contact_id")
		if contactID == "" {
			http.Error(w, "contact_id required", http.StatusBadRequest)
			return
		}

		msgs, err := app.MessageStore().GetConversation(r.Context(), selfID, contactID, 50)
		if err != nil {
			log.Printf("History fetch error: %v", err)
			http.Error(w, "failed to fetch history", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msgs)
	}))

	// 8.2 Contacts Persistence — manually add a contact
	mux.HandleFunc("/api/contacts/add", cors(func(w http.ResponseWriter, r *http.Request) {
		selfID, _ := resolveUserContext(r, app.JWTAuth())
		if selfID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			ContactID string `json:"contact_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer dbConn.Close()

		_, err = dbConn.Exec(`
			INSERT OR IGNORE INTO kuruvi_contacts (user_id, contact_id, last_msg, updated_at) 
			VALUES (?, ?, ?, ?)`, 
			selfID, req.ContactID, "New contact", time.Now())
		if err != nil {
			http.Error(w, "persistence error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// 8.3 Media Upload — handle file uploads
	mux.HandleFunc("/api/upload", cors(func(w http.ResponseWriter, r *http.Request) {
		selfID, _ := resolveUserContext(r, app.JWTAuth())
		if selfID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// 500MB limit for rich media (Video/Audio/Docs)
		err := r.ParseMultipartForm(500 << 20)
		if err != nil {
			http.Error(w, "file too large (max 500MB)", http.StatusRequestEntityTooLarge)
			return
		}

		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file upload error", http.StatusBadRequest)
			return
		}
		defer file.Close()

		ext := filepath.Ext(handler.Filename)
		fileName := fmt.Sprintf("%d-%d%s", time.Now().UnixNano(), rand.Intn(1000), ext)
		// Store in kuruvi/backend/data/attachments
		uploadDir := filepath.Join("kuruvi", "backend", "data", "attachments")
		os.MkdirAll(uploadDir, 0755)
		
		uploadPath := filepath.Join(uploadDir, fileName)

		dst, err := os.Create(uploadPath)
		if err != nil {
			log.Printf("Upload destination error: %v", err)
			http.Error(w, "internal save error", http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, file); err != nil {
			http.Error(w, "copy error", http.StatusInternalServerError)
			return
		}

		// Return relative URL so clients can resolve it based on their connection origin
		url := fmt.Sprintf("/attachments/%s", fileName)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"url": url})
	}))

	// 8.5 Account Deletion — permanent removal
	mux.HandleFunc("/api/auth/delete", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		selfID, _ := resolveUserContext(r, app.JWTAuth())
		if selfID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		log.Printf("[Auth] Processing deletion request for user: %s", selfID)

		// 1. Delete Kuruvi-specific data first (profiles, contacts)
		dbConn, err := sql.Open("sqlite", cfg.DatabaseDSN)
		if err == nil {
			defer dbConn.Close()
			dbConn.Exec("DELETE FROM kuruvi_profiles WHERE user_id = ?", selfID)
			dbConn.Exec("DELETE FROM kuruvi_contacts WHERE user_id = ? OR contact_id = ?", selfID, selfID)
		}

		// 2. Delete core account via framework
		ctx := r.Context()
		if err := app.AccountStore().DeleteUser(ctx, selfID); err != nil {
			log.Printf("[Auth] Core account deletion failed: %v", err)
			http.Error(w, "Failed to delete account", http.StatusInternalServerError)
			return
		}

		log.Printf("[Auth] User %s deleted successfully", selfID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	}))

	// 8.4 Serve Attachments
	attachmentsDir := filepath.Join("kuruvi", "backend", "data", "attachments")
	mux.Handle("/attachments/", http.StripPrefix("/attachments/", http.FileServer(http.Dir(attachmentsDir))))

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

// sendWelcomeEmail handles outbound SMTP dispatch using wneessen/go-mail.
func sendWelcomeEmail(toEmail, kuruviID string) {
	// Attempt to load SMTP from environment variables (supporting both KURUVI_ and generic prefixes)
	smtpHost := os.Getenv("KURUVI_SMTP_HOST")
	if smtpHost == "" {
		smtpHost = os.Getenv("SMTP_HOST")
	}
	smtpPort := os.Getenv("KURUVI_SMTP_PORT")
	if smtpPort == "" {
		smtpPort = os.Getenv("SMTP_PORT")
	}
	smtpUser := os.Getenv("KURUVI_SMTP_USER")
	if smtpUser == "" {
		smtpUser = os.Getenv("SMTP_USER")
	}
	smtpPass := os.Getenv("KURUVI_SMTP_PASS")
	if smtpPass == "" {
		smtpPass = os.Getenv("SMTP_PASS")
	}

	if smtpHost == "" {
		log.Printf("[Email Service] SMTP configuration (KURUVI_SMTP_HOST) missing. Intercepting delivery to %s for ID: %s", toEmail, kuruviID)
		return
	}

	m := mail.NewMsg()
	// Gmail requires the 'From' address to match the authenticated account
	fromAddr := smtpUser
	if fromAddr == "" {
		fromAddr = "noreply@kuruvi.mana"
	}
	if err := m.From(fromAddr); err != nil {
		log.Printf("[Email Service] Failed to set From address %s: %v", fromAddr, err)
		return
	}

	if err := m.To(toEmail); err != nil {
		log.Printf("[Email Service] Failed to validate recipient address %s: %v", toEmail, err)
		return
	}

	m.Subject("Welcome to Kuruvi! Here is your Unique Connect ID")

	body := fmt.Sprintf("Your account is ready.\n\nYour unique Kuruvi ID is: %s\n\nShare this ID so people can connect with you securely on the messaging platform.", kuruviID)
	m.SetBodyString(mail.TypeTextPlain, body)

	port := 587
	if smtpPort != "" {
		fmt.Sscanf(smtpPort, "%d", &port)
	}

	// Instantiate SMTP payload and execute
	c, err := mail.NewClient(smtpHost, mail.WithPort(port), mail.WithSMTPAuth(mail.SMTPAuthPlain), mail.WithUsername(smtpUser), mail.WithPassword(smtpPass))
	if err != nil {
		log.Printf("[Email Service] Fatal error instantiating mail client: %v", err)
		return
	}

	if err := c.DialAndSend(m); err != nil {
		log.Printf("[Email Service] Fatal error dialing SMTP transmission: %v", err)
	} else {
		log.Printf("[Email Service] Successfully transmitted welcome connection code to %s", toEmail)
	}
}

// Ensure context is used (resolveUserContext is a helper available for future endpoints)
var _ = context.Background
