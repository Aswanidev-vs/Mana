package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/auth"
	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/e2ee"
)

func main() {
	// ================================================================
	// 1. Configuration — every PRD feature enabled
	// ================================================================
	cfg := core.DefaultConfig()
	cfg.Port = 8080
	cfg.Host = "0.0.0.0"
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.EnableAuth = true
	cfg.JWTSecret = "mana-demo-secret-key-32bytes!!!!" // >= 32 bytes
	cfg.JWTIssuer = "mana-full-example"
	cfg.JWTExpiry = 24 * time.Hour
	cfg.AllowedOrigins = []string{"*"}
	cfg.RateLimitPerSecond = 100
	cfg.RateLimitBurst = 200
	cfg.MessageStorePath = "data/messages.json"
	cfg.ProductStorePath = "data/product.json"
	cfg.AttachmentDir = "data/attachments"
	cfg.STUNServers = []string{
		"stun:stun.l.google.com:19302",
		"stun:stun1.l.google.com:19302",
	}

	app := mana.New(cfg)

	// ================================================================
	// 2. HTTP routes — login, register, static files, metrics
	// ================================================================
	mux := app.Mux() // use the framework's ServeMux

	// Serve the frontend client
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "client.html")
	})

	// --- Auth endpoints ---
	jwtAuth := app.JWTAuth()

	type userRecord struct {
		Username string
		Password string // plain-text for demo only
		Role     auth.Role
	}
	users := map[string]userRecord{
		"alice": {"alice", "password123", auth.RoleUser},
		"bob":   {"bob", "password123", auth.RoleUser},
		"admin": {"admin", "adminpass", auth.RoleAdmin},
	}
	profileStore := app.ProfileStore()
	ctx := context.Background()
	for username, user := range users {
		_ = profileStore.UpsertProfile(ctx, core.UserProfile{
			UserID:      username,
			DisplayName: user.Username,
			UpdatedAt:   time.Now(),
		})
	}

	mux.HandleFunc("/api/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req.Username == "" || req.Password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}
		if _, exists := users[req.Username]; exists {
			http.Error(w, "username taken", http.StatusConflict)
			return
		}
		users[req.Username] = userRecord{req.Username, req.Password, auth.RoleUser}

		token, err := jwtAuth.GenerateToken(req.Username, req.Username, auth.RoleUser)
		if err != nil {
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	})

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		u, ok := users[req.Username]
		if !ok || u.Password != req.Password {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		token, err := jwtAuth.GenerateToken(u.Username, u.Username, u.Role)
		if err != nil {
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":    token,
			"username": u.Username,
			"role":     string(u.Role),
		})
	})

	// --- Room REST API ---
	mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
		rooms := app.RoomManager().List()
		json.NewEncoder(w).Encode(rooms)
	})

	requireAuth := func(w http.ResponseWriter, r *http.Request) (string, auth.Role, bool) {
		tokenStr := auth.ExtractToken(r)
		if tokenStr == "" {
			tokenStr = auth.ExtractTokenFromQuery(r)
		}
		if tokenStr == "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return "", "", false
		}
		claims, err := jwtAuth.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return "", "", false
		}
		return claims.UserID, claims.Role, true
	}

	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		userID, _, ok := requireAuth(w, r)
		if !ok {
			return
		}
		switch r.Method {
		case http.MethodGet:
			profile, _ := app.ProfileStore().GetProfile(r.Context(), userID)
			json.NewEncoder(w).Encode(profile)
		case http.MethodPost:
			var p core.UserProfile
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			p.UserID = userID
			if p.DisplayName == "" {
				p.DisplayName = userID
			}
			if err := app.ProfileStore().UpsertProfile(r.Context(), p); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(p)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/contacts", func(w http.ResponseWriter, r *http.Request) {
		userID, _, ok := requireAuth(w, r)
		if !ok {
			return
		}
		switch r.Method {
		case http.MethodGet:
			contacts, _ := app.ContactStore().GetContacts(r.Context(), userID)
			json.NewEncoder(w).Encode(map[string]any{"contacts": contacts})
		case http.MethodPost:
			var req struct {
				ContactID string `json:"contact_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ContactID == "" {
				http.Error(w, "contact_id required", http.StatusBadRequest)
				return
			}
			if err := app.ContactStore().AddContact(r.Context(), userID, req.ContactID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			contacts, _ := app.ContactStore().GetContacts(r.Context(), userID)
			json.NewEncoder(w).Encode(map[string]any{"contacts": contacts})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// --- E2EE public-key endpoints ---
	keyExchange := app.KeyExchange()
	if keyExchange != nil {
		mux.HandleFunc("/api/e2ee/pubkey", func(w http.ResponseWriter, r *http.Request) {
			peerID := r.URL.Query().Get("peer_id")
			if peerID == "" {
				http.Error(w, "peer_id required", http.StatusBadRequest)
				return
			}
			switch r.Method {
			case http.MethodGet:
				pub, ok := keyExchange.GetPublicKey(peerID)
				if !ok {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"peer_id": peerID, "public_key": pub})
			case http.MethodPost:
				var body struct {
					PublicKey []byte `json:"public_key"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "bad json", http.StatusBadRequest)
					return
				}
				keyExchange.StorePublicKey(peerID, body.PublicKey)
				w.WriteHeader(http.StatusCreated)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})
	}

	// --- Metrics JSON endpoint ---
	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		m := app.Metrics()
		m.UpdateRooms(int64(len(app.RoomManager().List())))
		m.UpdatePeerConnections(int64(app.SignalHub().PeerCount()))
		m.UpdateCalls(int64(app.CallManager().ActiveCallCount()))
		json.NewEncoder(w).Encode(m.Snapshot())
	})

	// --- Demo token endpoint (auto-login for testing) ---
	mux.HandleFunc("/api/demo-token", func(w http.ResponseWriter, r *http.Request) {
		username := r.URL.Query().Get("user")
		if username == "" {
			username = "alice"
		}
		role := auth.RoleUser
		if u, ok := users[username]; ok {
			role = u.Role
		}
		token, err := jwtAuth.GenerateToken(username, username, role)
		if err != nil {
			http.Error(w, "token error", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":    token,
			"username": username,
			"role":     string(role),
		})
	})

	// ================================================================
	// 3. Event hooks — the framework calls these on lifecycle events
	// ================================================================

	app.OnUserJoin(func(roomID string, user core.User) {
		log.Printf("[JOIN] room=%s user=%s (%s)", roomID, user.Username, user.ID)
	})

	app.OnUserLeave(func(roomID string, user core.User) {
		log.Printf("[LEAVE] room=%s user=%s", roomID, user.Username)
	})

	app.OnMessage(func(msg core.Message) {
		log.Printf("[MSG] room=%s from=%s payload=%s", msg.RoomID, msg.SenderID, string(msg.Payload))
	})

	app.OnCallStart(func(event core.CallEvent) {
		log.Printf("[CALL START] room=%s caller=%s type=%s", event.RoomID, event.Caller, event.Type)
	})

	app.OnCallEnd(func(event core.CallEvent) {
		log.Printf("[CALL END] room=%s", event.RoomID)
	})

	// Signaling handlers for video control
	app.OnSignal(core.SignalMute, func(sig core.Signal) {
		log.Printf("[MUTE] peer=%s room=%s", sig.From, sig.RoomID)
	})

	app.OnSignal(core.SignalCameraToggle, func(sig core.Signal) {
		log.Printf("[CAMERA] peer=%s room=%s", sig.From, sig.RoomID)
	})

	app.OnSignal(core.SignalPin, func(sig core.Signal) {
		log.Printf("[PIN] peer=%s room=%s", sig.From, sig.RoomID)
	})

	// ================================================================
	// 4. E2EE demo: generate a test key pair so the /ws key_exchange
	//    flow works out of the box for the demo client
	// ================================================================
	if keyExchange != nil {
		// Pre-register demo peer public keys so the E2EE handshake demo works
		genDemoKeys := func(peerID string) {
			kx := e2ee.NewX25519KeyExchange()
			kp, err := kx.GenerateKeyPair(peerID)
			if err != nil {
				log.Printf("E2EE demo key gen failed for %s: %v", peerID, err)
				return
			}
			keyExchange.StorePublicKey(peerID, kp.PublicKey)
		}
		genDemoKeys("demo-alice")
		genDemoKeys("demo-bob")
		log.Println("E2EE demo keys registered for demo-alice and demo-bob")
	}

	// ================================================================
	// 5. Start server with graceful shutdown
	// ================================================================
	log.Printf("Mana full example starting on http://%s:%d", cfg.Host, cfg.Port)
	log.Println("Routes:")
	log.Println("  GET  /                — frontend client")
	log.Println("  POST /api/login       — JWT login")
	log.Println("  POST /api/register    — JWT registration")
	log.Println("  GET  /api/demo-token  — auto-login for testing (?user=alice)")
	log.Println("  GET  /api/rooms       — list rooms")
	log.Println("  GET  /api/metrics     — Prometheus-compatible metrics JSON")
	log.Println("  GET  /health          — health check")
	log.Println("  GET  /metrics         — Prometheus text exposition")
	log.Println("  WS   /ws?token=...    — main WebSocket endpoint")
	log.Println("")
	log.Println("Demo users: alice/password123  bob/password123  admin/adminpass")

	// Build the HTTP server manually so we can add our own shutdown logic
	// alongside the framework's WebSocket endpoint.
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("Server error: %v", err)
	case sig := <-quit:
		log.Printf("Received %v, shutting down...", sig)
		_ = srv.Close()
	}

	log.Println("Mana full example stopped.")
}
