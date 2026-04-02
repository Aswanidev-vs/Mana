package main

import (
	"log"

	"github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	cfg := core.DefaultConfig()
	cfg.Port = 8080
	cfg.Host = "localhost"
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.AllowedOrigins = []string{"*"} // Allow all origins for development

	app := mana.New(cfg)

	app.OnMessage(func(msg core.Message) {
		log.Printf("[%s] %s: %s", msg.RoomID, msg.SenderID, string(msg.Payload))
	})

	app.OnUserJoin(func(roomID string, user core.User) {
		log.Printf("User %s joined room %s", user.Username, roomID)
	})

	app.OnUserLeave(func(roomID string, user core.User) {
		log.Printf("User %s left room %s", user.Username, roomID)
	})

	app.OnCallStart(func(event core.CallEvent) {
		log.Printf("Call started in room %s by %s", event.RoomID, event.Caller)
	})

	app.OnCallEnd(func(event core.CallEvent) {
		log.Printf("Call ended in room %s", event.RoomID)
	})

	// Use graceful shutdown to handle SIGINT/SIGTERM
	if err := app.StartWithGracefulShutdown(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
