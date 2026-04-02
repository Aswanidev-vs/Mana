package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	// 1. Initialize Mana with default config
	cfg := core.DefaultConfig()
	cfg.Port = 8081
	cfg.EnableAuth = false // Simplified for example

	app := mana.New(cfg)

	// 2. Add an endpoint to trigger a notification
	// In a real app, this might be triggered by a database hook or another service.
	app.Mux().HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user")
		if userID == "" {
			http.Error(w, "user query param required", http.StatusBadRequest)
			return
		}

		// Send a notification via the NotificationHub
		err := app.NotificationHub().Send(context.Background(), userID, core.Notification{
			ID:    fmt.Sprintf("notif-%d", time.Now().UnixNano()),
			Title: "Example Notification",
			Body:  "This is a plug-and-play notification from Mana!",
			Data: map[string]interface{}{
				"priority": "high",
				"sent_at":  time.Now().Format(time.RFC3339),
			},
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		fmt.Fprintf(w, "Notification sent to %s", userID)
	})

	// 3. Static frontend to test it
	app.Mux().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `
			<!DOCTYPE html>
			<html>
			<head><title>Mana Notif Demo</title></head>
			<body style="font-family:sans-serif; padding: 2rem;">
				<h1>Mana Notification Demo</h1>
				<p>Open the console to see the notification message.</p>
				<button onclick="connect()">Connect as 'alice'</button>
				<button onclick="trigger()">Notify 'alice'</button>
				<div id="log" style="margin-top: 1rem; padding: 1rem; background: #eee; border-radius: 4px;"></div>

				<script>
					let ws;
					function connect() {
						ws = new WebSocket('ws://' + location.host + '/ws?user=alice');
						ws.onopen = () => log('Connected to Mana');
						ws.onmessage = (e) => {
							const msg = JSON.parse(e.data);
							log('Received: ' + JSON.stringify(msg, null, 2));
							if(msg.type === 'notification') {
								alert(msg.title + ": " + msg.body);
							}
						};
					}
					function trigger() {
						fetch('/notify?user=alice')
							.then(r => r.text())
							.then(t => log('Server said: ' + t));
					}
					function log(m) {
						const d = document.getElementById('log');
						d.innerHTML += '<div>[' + new Date().toLocaleTimeString() + '] ' + m + '</div>';
					}
				</script>
			</body>
			</html>
		`)
	})

	// 4. Start the server
	log.Printf("Notification demo starting on http://localhost:8081\n")
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
