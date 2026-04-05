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
	"github.com/Aswanidev-vs/mana/storage/db"
	_ "modernc.org/sqlite" // Import SQLite driver
)

func main() {
	// ================================================================
	// 1. Configuration — Modern "Golden Path"
	// ================================================================
	cfg := core.DefaultConfig()
	cfg.Port = 8080
	cfg.Host = "0.0.0.0"
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.EnableAuth = true
	cfg.JWTSecret = "mana-demo-secret-key-32bytes-min!!!!"
	cfg.JWTIssuer = "mana-full-example"
	cfg.AllowedOrigins = []string{"*"}
	
	// Plug-and-play SQL Battery (SQLite)
	cfg.DatabaseDriver = db.SQLite
	cfg.DatabaseDSN = "data/mana-full.db"
	cfg.AttachmentDir = "data/attachments"

	os.MkdirAll("data/attachments", 0755)

	app := mana.New(cfg)

	// ================================================================
	// 2. HTTP Routes & Backend Logic
	// ================================================================
	mux := app.Mux() 

	// Static frontend
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "client.html")
	})

	jwtAuth := app.JWTAuth()

	// Simple User Management
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		
		// In a real app, verify against app.AccountStore()
		token, err := jwtAuth.GenerateToken(req.Username, req.Username, auth.RoleUser)
		if err != nil {
			http.Error(w, "token error", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"token": token, "username": req.Username})
	})

	// --- Metrics & Health ---
	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		m := app.Metrics()
		// Manual updates for demo
		m.UpdateRooms(int64(len(app.RoomManager().List())))
		m.UpdatePeerConnections(int64(app.SignalHub().PeerCount()))
		json.NewEncoder(w).Encode(m.Snapshot())
	})

	// ================================================================
	// 3. Framework Event Hooks
	// ================================================================
	app.OnUserJoin(func(roomID string, user core.User) {
		log.Printf("[JOIN] room=%s user=%s (UUID=%s)", roomID, user.Username, user.ID)
	})

	app.OnMessage(func(msg core.Message) {
		log.Printf("[MSG] room=%s from=%s length=%d", msg.RoomID, msg.SenderID, len(msg.Payload))
	})

	app.OnCallStart(func(event core.CallEvent) {
		log.Printf("[CALL START] room=%s caller=%s type=%s", event.RoomID, event.Caller, event.Type)
	})

	app.OnCallEnd(func(event core.CallEvent) {
		log.Printf("[CALL END] room=%s", event.RoomID)
	})

	// ================================================================
	// 4. Start Server with Graceful Shutdown
	// ================================================================
	log.Printf("Mana full example starting on http://%s:%d", cfg.Host, cfg.Port)
	
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit
	log.Println("Shutting down...")
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Mana full example stopped.")
}
