package main

import (
	"log"
	"net/http"

	"github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	// Configure the SFU Server
	cfg := core.DefaultConfig()
	cfg.Port = 9000
	cfg.Host = "localhost"
	cfg.EnableRTC = true  // Enable WebRTC SFU
	cfg.EnableE2EE = true // End-to-End Encryption support
	cfg.AllowedOrigins = []string{"*"}

	// Initialize the Mana SFU App
	app := mana.New(cfg)

	// Logging: Track room activity
	app.OnUserJoin(func(roomID string, user core.User) {
		log.Printf("📥 [%s] User Joined: %s (%s)", roomID, user.Username, user.ID)
	})

	app.OnUserLeave(func(roomID string, user core.User) {
		log.Printf("📤 [%s] User Left: %s", roomID, user.Username)
	})

	// WebRTC: Logic for call lifecycle
	app.OnCallStart(func(event core.CallEvent) {
		log.Printf("📹 [%s] Video Call Started by %s", event.RoomID, event.Caller)
	})

	// Message Handler: Chat integration
	app.OnMessage(func(msg core.Message) {
		log.Printf("💬 [%s] %s: %s", msg.RoomID, msg.SenderID, string(msg.Payload))
	})

	// Serve a simple status page or dashboard if needed
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Mana SFU is running smoothly!"))
	})

	log.Printf("🚀 Mana SFU Server starting on %s:%d", cfg.Host, cfg.Port)

	// Start with graceful shutdown
	if err := app.StartWithGracefulShutdown(); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}
