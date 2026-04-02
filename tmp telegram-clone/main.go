package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/auth"
	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/e2ee"
	"github.com/Aswanidev-vs/mana/observ"
)

// ================================================================
// Data stores
// ================================================================

var (
	users          = make(map[string]*UserRecord)
	usersMu        sync.RWMutex
	contacts       = make(map[string]map[string]bool)
	contactsMu     sync.RWMutex
	groups         = make(map[string]*GroupRecord)
	groupsMu       sync.RWMutex
	e2eeBundles    = make(map[string]*e2ee.PreKeyBundle)
	e2eeBundlesMu  sync.RWMutex
	dmRooms        = make(map[string]*DMRecord)
	dmRoomsMu      sync.RWMutex
	messages       = make(map[string][]StoredMessage)
	messagesMu     sync.RWMutex
	deliveryStatus = make(map[string]map[string]string)
	deliveryMu     sync.RWMutex
	logger         *observ.Logger
)

type UserRecord struct {
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	Role      auth.Role `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	Members   []string  `json:"members"`
	CreatedAt time.Time `json:"created_at"`
}

type DMRecord struct {
	RoomID    string    `json:"room_id"`
	Users     []string  `json:"users"`
	CreatedAt time.Time `json:"created_at"`
}

type StoredMessage struct {
	ID        string    `json:"id"`
	RoomID    string    `json:"room_id"`
	From      string    `json:"from"`
	Payload   []int     `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

func dmKey(u1, u2 string) string {
	if u1 < u2 {
		return u1 + ":" + u2
	}
	return u2 + ":" + u1
}

func dmRoomID(u1, u2 string) string {
	return "dm-" + dmKey(u1, u2)
}

func storeMessage(roomID, from string, payload []int) string {
	msgID := fmt.Sprintf("msg-%d", time.Now().UnixNano())
	sm := StoredMessage{ID: msgID, RoomID: roomID, From: from, Payload: payload, Timestamp: time.Now()}
	messagesMu.Lock()
	messages[roomID] = append(messages[roomID], sm)
	messagesMu.Unlock()
	deliveryMu.Lock()
	if deliveryStatus[msgID] == nil {
		deliveryStatus[msgID] = make(map[string]string)
	}
	deliveryStatus[msgID][from] = "read"
	deliveryMu.Unlock()
	return msgID
}

func markDelivered(msgID, user string) {
	deliveryMu.Lock()
	if deliveryStatus[msgID] == nil {
		deliveryStatus[msgID] = make(map[string]string)
	}
	if deliveryStatus[msgID][user] != "read" {
		deliveryStatus[msgID][user] = "delivered"
	}
	deliveryMu.Unlock()
}

func markRead(msgID, user string) {
	deliveryMu.Lock()
	if deliveryStatus[msgID] == nil {
		deliveryStatus[msgID] = make(map[string]string)
	}
	deliveryStatus[msgID][user] = "read"
	deliveryMu.Unlock()
}

func getDeliveryStatus(msgID string) string {
	deliveryMu.RLock()
	statuses := deliveryStatus[msgID]
	deliveryMu.RUnlock()
	if len(statuses) == 0 {
		return "sent"
	}
	allDelivered := true
	allRead := true
	for _, s := range statuses {
		if s == "" || s == "sent" {
			allDelivered = false
			allRead = false
		} else if s == "delivered" {
			allRead = false
		}
	}
	if allRead {
		return "read"
	}
	if allDelivered {
		return "delivered"
	}
	return "sent"
}

func getRoomMembers(roomID string) []string {
	if strings.HasPrefix(roomID, "dm-") {
		key := strings.TrimPrefix(roomID, "dm-")
		dmRoomsMu.RLock()
		defer dmRoomsMu.RUnlock()
		if dm, ok := dmRooms[key]; ok {
			return dm.Users
		}
		return nil
	}
	groupsMu.RLock()
	defer groupsMu.RUnlock()
	if g, ok := groups[roomID]; ok {
		return g.Members
	}
	return nil
}

func activeSessionIDsForUser(app *mana.App, username string) []string {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" {
		return nil
	}

	sessions := app.SignalHub().UserPeerIDs(username)
	if len(sessions) == 0 {
		return nil
	}

	ids := make([]string, 0, len(sessions))
	for _, id := range sessions {
		ids = append(ids, id)
	}
	return ids
}

func isUserOnline(app *mana.App, username string) bool {
	return len(activeSessionIDsForUser(app, username)) > 0
}

func listOnlineUsers(app *mana.App) []string {
	usersMu.RLock()
	defer usersMu.RUnlock()

	online := make([]string, 0, len(users))
	for name := range users {
		if isUserOnline(app, name) {
			online = append(online, name)
		}
	}
	return online
}

func attachUserToRoom(app *mana.App, roomID, username string) {
	for _, sessionID := range activeSessionIDsForUser(app, username) {
		app.SignalHub().AddPeerToRoom(roomID, sessionID)
	}
}

func notifyUserPresence(app *mana.App, fromUser, toUser string, online bool) {
	ctx := context.Background()
	presenceJSON, _ := json.Marshal(map[string]interface{}{
		"type":     "presence",
		"username": fromUser,
		"online":   online,
	})
	_ = app.SignalHub().Send(ctx, core.Signal{
		Type:    core.SignalType("presence"),
		From:    fromUser,
		To:      toUser,
		Payload: presenceJSON,
	})
}

// ================================================================
// Main
// ================================================================

func main() {
	cfg := core.DefaultConfig()
	cfg.Port = 8080
	cfg.Host = "localhost"
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.EnableAuth = true
	cfg.JWTSecret = "telegram-clone-secret-key-32bytes!!"
	cfg.JWTIssuer = "telegram-clone"
	cfg.JWTExpiry = 24 * time.Hour
	cfg.AllowedOrigins = []string{"*"}
	cfg.RateLimitPerSecond = 100
	cfg.RateLimitBurst = 200

	app := mana.New(cfg)
	mux := app.Mux()
	jwtAuth := app.JWTAuth()
	keyExchange := app.KeyExchange()

	logger = app.Logger()
	apiLog := logger.WithComponent("api")

	// ================================================================
	// Seed demo data
	// ================================================================

	seedUsers := []*UserRecord{
		{"alice", "password123", auth.RoleUser, time.Now()},
		{"bob", "password123", auth.RoleUser, time.Now()},
		{"charlie", "password123", auth.RoleUser, time.Now()},
		{"dave", "password123", auth.RoleUser, time.Now()},
		{"eve", "password123", auth.RoleUser, time.Now()},
		{"admin", "adminpass", auth.RoleAdmin, time.Now()},
	}
	usersMu.Lock()
	for _, u := range seedUsers {
		users[u.Username] = u
	}
	usersMu.Unlock()

	contactsMu.Lock()
	allDemos := []string{"alice", "bob", "charlie", "dave", "eve"}
	for _, u := range allDemos {
		if contacts[u] == nil {
			contacts[u] = make(map[string]bool)
		}
		for _, other := range allDemos {
			if other != u {
				contacts[u][other] = true
			}
		}
	}
	contactsMu.Unlock()

	for _, u := range allDemos {
		identity, err := e2ee.NewX3DHIdentity()
		if err != nil {
			continue
		}
		identity.GenerateOneTimePreKeys(10)
		e2eeBundlesMu.Lock()
		e2eeBundles[u] = identity.GetBundle()
		e2eeBundlesMu.Unlock()
	}
	apiLog.Info("Seeded %d users with E2EE bundles", len(seedUsers))

	// ================================================================
	// Signal handler for "sync" — auto-join user to all their rooms
	// ================================================================

	app.SignalHub().On(core.SignalType("sync"), func(sig core.Signal) {
		username := sig.From
		if username == "" {
			return
		}

		apiLog.Info("SYNC signal received for user: %s", username)

		ctx := context.Background()

		// Auto-join all DM rooms
		dmRoomsMu.RLock()
		for key, dm := range dmRooms {
			parts := strings.Split(key, ":")
			if len(parts) == 2 && (parts[0] == username || parts[1] == username) {
				apiLog.Debug("Auto-joining DM room: %s", dm.RoomID)
				app.RoomManager().Create(dm.RoomID, "dm:"+key)
				attachUserToRoom(app, dm.RoomID, username)

				// Mark undelivered messages as delivered
				messagesMu.RLock()
				for _, msg := range messages[dm.RoomID] {
					if msg.From != username {
						markDelivered(msg.ID, username)
					}
				}
				messagesMu.RUnlock()
			}
		}
		dmRoomsMu.RUnlock()

		// Auto-join all group rooms
		groupsMu.RLock()
		for gid, g := range groups {
			for _, m := range g.Members {
				if m == username {
					apiLog.Debug("Auto-joining group room: %s", gid)
					app.RoomManager().Create(gid, g.Name)
					attachUserToRoom(app, gid, username)

					messagesMu.RLock()
					for _, msg := range messages[gid] {
						if msg.From != username {
							markDelivered(msg.ID, username)
						}
					}
					messagesMu.RUnlock()
					break
				}
			}
		}
		groupsMu.RUnlock()

		apiLog.Info("Synced rooms for %s", username)

		// Send a "sync_complete" notification to the user
		app.NotificationHub().Send(ctx, username, core.Notification{
			ID:    fmt.Sprintf("sync-%d", time.Now().UnixNano()),
			Title: "Sync Complete",
			Body:  "Your chat history has been synchronized.",
			Data: map[string]interface{}{
				"type": "sync_complete",
			},
		})

		// Broadcast online presence to contacts
		contactsMu.RLock()
		userContacts := contacts[username]
		contactsMu.RUnlock()
		if userContacts != nil {
			for contact := range userContacts {
				if isUserOnline(app, contact) {
					notifyUserPresence(app, username, contact, true)
				}
			}
		}
	})

	// Handle "mark_read" — mark all messages in a room as read
	app.SignalHub().On(core.SignalType("mark_read"), func(sig core.Signal) {
		reader := sig.From
		roomID := sig.RoomID
		if reader == "" || roomID == "" {
			return
		}

		messagesMu.RLock()
		msgs := messages[roomID]
		messagesMu.RUnlock()

		for _, msg := range msgs {
			if msg.From != reader {
				markRead(msg.ID, reader)
			}
		}
	})

	// OnMessage: store message + mark delivered + send confirmation
	// NOTE: The router already handles broadcasting to room members via BroadcastToRoom
	// So we just need to store it and track delivery status
	app.OnMessage(func(msg core.Message) {
		if msg.RoomID == "" {
			return
		}

		apiLog.Info("OnMessage received: room=%s from=%s", msg.RoomID, msg.SenderID)

		// Parse payload
		payload := make([]int, len(msg.Payload))
		for i, b := range msg.Payload {
			payload[i] = int(b)
		}
		sender := msg.SenderID

		// Store message
		msgID := storeMessage(msg.RoomID, sender, payload)
		apiLog.Info("Stored message %s in room %s", msgID, msg.RoomID)

		// Get room members and check who's online
		members := getRoomMembers(msg.RoomID)
		apiLog.Info("Room %s members: %v", msg.RoomID, members)

		apiLog.Info("Online users: %v", listOnlineUsers(app))
		for _, member := range members {
			if member == sender {
				continue
			}
			online := isUserOnline(app, member)
			apiLog.Info("Checking if %s is online: %v", member, online)
			if online {
				apiLog.Info("Marking message %s as delivered to %s", msgID, member)
				markDelivered(msgID, member)
			}
		}

		// Router already broadcasts via BroadcastToRoom - no need to do it again

		// Notify sender of delivery status via NotificationHub
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		status := getDeliveryStatus(msgID)
		apiLog.Info("Sending delivery confirmation to %s: %s", sender, status)

		app.NotificationHub().Send(ctx, sender, core.Notification{
			ID:    msgID + "-delivery",
			Type:  "delivery",
			Title: "Message Status",
			Data: map[string]interface{}{
				"msg_id": msgID,
				"status": status,
			},
		})

		// Also notify recipients who are online but might not be in the room
		for _, member := range members {
			if member == sender {
				continue
			}
			if isUserOnline(app, member) {
				app.NotificationHub().Send(ctx, member, core.Notification{
					ID:    msgID,
					Type:  "new_message",
					Title: "New Message",
					Body:  fmt.Sprintf("New message from %s", sender),
					Data: map[string]interface{}{
						"room_id": msg.RoomID,
						"from":    sender,
						"msg_id":  msgID,
					},
				})
			}
		}
	})

	// ================================================================
	// Static file serving
	// ================================================================
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.ServeFile(w, r, "frontend/index.html")
			return
		}
		http.NotFound(w, r)
	})

	// ================================================================
	// Auth endpoints
	// ================================================================

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
		req.Username = strings.TrimSpace(strings.ToLower(req.Username))
		if req.Username == "" || req.Password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}
		if len(req.Username) < 3 {
			http.Error(w, "username must be at least 3 characters", http.StatusBadRequest)
			return
		}
		if len(req.Password) < 6 {
			http.Error(w, "password must be at least 6 characters", http.StatusBadRequest)
			return
		}
		usersMu.Lock()
		if _, exists := users[req.Username]; exists {
			usersMu.Unlock()
			http.Error(w, "username taken", http.StatusConflict)
			return
		}
		users[req.Username] = &UserRecord{req.Username, req.Password, auth.RoleUser, time.Now()}
		usersMu.Unlock()
		contactsMu.Lock()
		if contacts[req.Username] == nil {
			contacts[req.Username] = make(map[string]bool)
		}
		contactsMu.Unlock()
		identity, err := e2ee.NewX3DHIdentity()
		if err == nil {
			identity.GenerateOneTimePreKeys(10)
			e2eeBundlesMu.Lock()
			e2eeBundles[req.Username] = identity.GetBundle()
			e2eeBundlesMu.Unlock()
		}
		token, _ := jwtAuth.GenerateToken(req.Username, req.Username, auth.RoleUser)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token, "username": req.Username, "role": "user"})
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
		req.Username = strings.TrimSpace(strings.ToLower(req.Username))
		usersMu.RLock()
		u, ok := users[req.Username]
		usersMu.RUnlock()
		if !ok || u.Password != req.Password {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		token, _ := jwtAuth.GenerateToken(u.Username, u.Username, u.Role)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token, "username": u.Username, "role": string(u.Role)})
	})

	mux.HandleFunc("/api/demo-token", func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("user")))
		if username == "" {
			username = "alice"
		}
		usersMu.RLock()
		u, ok := users[username]
		usersMu.RUnlock()
		if !ok {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		token, _ := jwtAuth.GenerateToken(u.Username, u.Username, u.Role)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token, "username": u.Username, "role": string(u.Role)})
	})

	// ================================================================
	// User search
	// ================================================================

	mux.HandleFunc("/api/users/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
		currentUser := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("me")))
		usersMu.RLock()
		var results []map[string]interface{}
		for name, u := range users {
			if name == currentUser {
				continue
			}
			if q == "" || strings.Contains(name, q) {
				results = append(results, map[string]interface{}{"username": u.Username})
			}
		}
		usersMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	})

	// ================================================================
	// Contacts
	// ================================================================

	mux.HandleFunc("/api/contacts", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("user")))
			contactsMu.RLock()
			userContacts := contacts[username]
			contactsMu.RUnlock()
			var list []map[string]interface{}
			if userContacts != nil {
				for c := range userContacts {
					online := isUserOnline(app, c)
					list = append(list, map[string]interface{}{"username": c, "online": online})
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(list)
		case http.MethodPost:
			var req struct {
				User    string `json:"user"`
				Contact string `json:"contact"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			req.User = strings.TrimSpace(strings.ToLower(req.User))
			req.Contact = strings.TrimSpace(strings.ToLower(req.Contact))
			if req.User == req.Contact {
				http.Error(w, "cannot add yourself", http.StatusBadRequest)
				return
			}
			usersMu.RLock()
			_, exists := users[req.Contact]
			usersMu.RUnlock()
			if !exists {
				http.Error(w, "user not found", http.StatusNotFound)
				return
			}
			contactsMu.Lock()
			if contacts[req.User] == nil {
				contacts[req.User] = make(map[string]bool)
			}
			alreadyAdded := contacts[req.User][req.Contact]
			contacts[req.User][req.Contact] = true
			if contacts[req.Contact] == nil {
				contacts[req.Contact] = make(map[string]bool)
			}
			contacts[req.Contact][req.User] = true
			contactsMu.Unlock()
			if alreadyAdded {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "already_added"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case http.MethodDelete:
			var req struct {
				User    string `json:"user"`
				Contact string `json:"contact"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			req.User = strings.TrimSpace(strings.ToLower(req.User))
			req.Contact = strings.TrimSpace(strings.ToLower(req.Contact))
			contactsMu.Lock()
			if contacts[req.User] != nil {
				delete(contacts[req.User], req.Contact)
			}
			if contacts[req.Contact] != nil {
				delete(contacts[req.Contact], req.User)
			}
			contactsMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// ================================================================
	// DM rooms
	// ================================================================

	mux.HandleFunc("/api/dm", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			User    string `json:"user"`
			Contact string `json:"contact"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.User = strings.TrimSpace(strings.ToLower(req.User))
		req.Contact = strings.TrimSpace(strings.ToLower(req.Contact))
		contactsMu.RLock()
		isContact := contacts[req.User] != nil && contacts[req.User][req.Contact]
		contactsMu.RUnlock()
		if !isContact {
			http.Error(w, "not a contact", http.StatusForbidden)
			return
		}
		key := dmKey(req.User, req.Contact)
		roomID := dmRoomID(req.User, req.Contact)
		dmRoomsMu.Lock()
		if _, exists := dmRooms[key]; !exists {
			dmRooms[key] = &DMRecord{RoomID: roomID, Users: []string{req.User, req.Contact}, CreatedAt: time.Now()}
		}
		dm := dmRooms[key]
		dmRoomsMu.Unlock()

		// Create the room in the framework
		app.RoomManager().Create(roomID, "dm:"+key)

		// Also add them to the room so they receive messages
		attachUserToRoom(app, roomID, req.User)

		if isUserOnline(app, req.Contact) {
			// Contact is online, add them too
			attachUserToRoom(app, roomID, req.Contact)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dm)
	})

	mux.HandleFunc("/api/dm/list", func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("user")))
		dmRoomsMu.RLock()
		var list []map[string]interface{}
		for key, dm := range dmRooms {
			parts := strings.Split(key, ":")
			if len(parts) == 2 && (parts[0] == username || parts[1] == username) {
				otherUser := parts[0]
				if parts[0] == username {
					otherUser = parts[1]
				}
				unread := 0
				messagesMu.RLock()
				if msgs, ok := messages[dm.RoomID]; ok {
					for _, msg := range msgs {
						if msg.From != username {
							deliveryMu.RLock()
							ds := deliveryStatus[msg.ID]
							if ds == nil || ds[username] == "" || ds[username] == "sent" {
								unread++
							}
							deliveryMu.RUnlock()
						}
					}
				}
				messagesMu.RUnlock()
				list = append(list, map[string]interface{}{
					"room_id":    dm.RoomID,
					"other_user": otherUser,
					"unread":     unread,
				})
			}
		}
		dmRoomsMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	})

	// ================================================================
	// Groups
	// ================================================================

	mux.HandleFunc("/api/groups", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("user")))
			groupsMu.RLock()
			var list []map[string]interface{}
			for _, g := range groups {
				for _, m := range g.Members {
					if m == username {
						list = append(list, map[string]interface{}{
							"id":      g.ID,
							"name":    g.Name,
							"owner":   g.Owner,
							"members": g.Members,
						})
						break
					}
				}
			}
			groupsMu.RUnlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(list)
		case http.MethodPost:
			var req struct {
				Name    string   `json:"name"`
				Owner   string   `json:"owner"`
				Members []string `json:"members"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			if req.Name == "" || req.Owner == "" {
				http.Error(w, "name and owner required", http.StatusBadRequest)
				return
			}
			req.Owner = strings.ToLower(req.Owner)
			contactsMu.RLock()
			ownerContacts := contacts[req.Owner]
			contactsMu.RUnlock()
			members := []string{req.Owner}
			for _, m := range req.Members {
				m = strings.TrimSpace(strings.ToLower(m))
				if m == "" || m == req.Owner {
					continue
				}
				if ownerContacts == nil || !ownerContacts[m] {
					http.Error(w, fmt.Sprintf("user '%s' is not in your contacts", m), http.StatusForbidden)
					return
				}
				usersMu.RLock()
				_, exists := users[m]
				usersMu.RUnlock()
				if !exists {
					http.Error(w, fmt.Sprintf("user '%s' not found", m), http.StatusNotFound)
					return
				}
				members = append(members, m)
			}
			groupID := fmt.Sprintf("grp-%s-%d", strings.ReplaceAll(strings.ToLower(req.Name), " ", "-"), time.Now().UnixNano())
			g := &GroupRecord{ID: groupID, Name: req.Name, Owner: req.Owner, Members: members, CreatedAt: time.Now()}
			groupsMu.Lock()
			groups[groupID] = g
			groupsMu.Unlock()

			app.RoomManager().Create(groupID, req.Name)
			for _, member := range members {
				attachUserToRoom(app, groupID, member)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(g)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/groups/add-member", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			GroupID string `json:"group_id"`
			User    string `json:"user"`
			By      string `json:"by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.User = strings.TrimSpace(strings.ToLower(req.User))
		req.By = strings.TrimSpace(strings.ToLower(req.By))
		contactsMu.RLock()
		isContact := contacts[req.By] != nil && contacts[req.By][req.User]
		contactsMu.RUnlock()
		if !isContact {
			http.Error(w, "user is not in your contacts", http.StatusForbidden)
			return
		}
		groupsMu.Lock()
		g, ok := groups[req.GroupID]
		if !ok {
			groupsMu.Unlock()
			http.Error(w, "group not found", http.StatusNotFound)
			return
		}
		isAllowed := g.Owner == req.By
		if !isAllowed {
			for _, m := range g.Members {
				if m == req.By {
					isAllowed = true
					break
				}
			}
		}
		if !isAllowed {
			groupsMu.Unlock()
			http.Error(w, "not authorized", http.StatusForbidden)
			return
		}
		for _, m := range g.Members {
			if m == req.User {
				groupsMu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "already_member"})
				return
			}
		}
		g.Members = append(g.Members, req.User)
		groups[req.GroupID] = g
		groupsMu.Unlock()
		attachUserToRoom(app, req.GroupID, req.User)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// ================================================================
	// Message history & delivery
	// ================================================================

	mux.HandleFunc("/api/messages", func(w http.ResponseWriter, r *http.Request) {
		roomID := r.URL.Query().Get("room_id")
		username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("user")))
		if roomID == "" {
			http.Error(w, "room_id required", http.StatusBadRequest)
			return
		}
		messagesMu.RLock()
		msgs := messages[roomID]
		messagesMu.RUnlock()
		if username != "" {
			for _, msg := range msgs {
				if msg.From != username {
					markDelivered(msg.ID, username)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msgs)
	})

	mux.HandleFunc("/api/messages/read", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			RoomID   string `json:"room_id"`
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		messagesMu.RLock()
		msgs := messages[req.RoomID]
		messagesMu.RUnlock()

		sendersNotified := make(map[string]bool)
		ctx := context.Background()
		for _, msg := range msgs {
			if msg.From != req.Username {
				markRead(msg.ID, req.Username)
			}
		}
		for _, msg := range msgs {
			if msg.From != req.Username && isUserOnline(app, msg.From) && !sendersNotified[msg.From] {
				sendersNotified[msg.From] = true
				app.NotificationHub().Send(ctx, msg.From, core.Notification{
					ID:    fmt.Sprintf("read-%s-%d", req.RoomID, time.Now().UnixNano()),
					Type:  "read_receipt",
					Title: "Message Read",
					Data: map[string]interface{}{
						"room_id": req.RoomID,
						"reader":  req.Username,
					},
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/messages/delivery", func(w http.ResponseWriter, r *http.Request) {
		msgID := r.URL.Query().Get("msg_id")
		if msgID == "" {
			http.Error(w, "msg_id required", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"msg_id": msgID, "status": getDeliveryStatus(msgID)})
	})

	// ================================================================
	// E2EE
	// ================================================================

	mux.HandleFunc("/api/e2ee/bundle", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("user")))
			e2eeBundlesMu.RLock()
			bundle, ok := e2eeBundles[username]
			e2eeBundlesMu.RUnlock()
			if !ok {
				http.Error(w, "no bundle", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(bundle)
		case http.MethodPost:
			var bundle e2ee.PreKeyBundle
			if err := json.NewDecoder(r.Body).Decode(&bundle); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("user")))
			if username == "" {
				http.Error(w, "user required", http.StatusBadRequest)
				return
			}
			e2eeBundlesMu.Lock()
			e2eeBundles[username] = &bundle
			e2eeBundlesMu.Unlock()
			if keyExchange != nil {
				keyExchange.StorePublicKey(username, bundle.IdentityKey)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/e2ee/pubkey", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("user")))
			e2eeBundlesMu.RLock()
			bundle, ok := e2eeBundles[username]
			e2eeBundlesMu.RUnlock()
			if !ok {
				http.Error(w, "no key", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"username": username, "public_key": bundle.IdentityKey})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// ================================================================
	// Online, Rooms, Metrics
	// ================================================================

	mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
		rooms := app.RoomManager().List()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rooms)
	})

	mux.HandleFunc("/api/online", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listOnlineUsers(app))
	})

	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		m := app.Metrics()
		m.UpdateRooms(int64(len(app.RoomManager().List())))
		m.UpdatePeerConnections(int64(app.SignalHub().PeerCount()))
		m.UpdateCalls(int64(app.CallManager().ActiveCallCount()))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m.Snapshot())
	})

	// ================================================================
	// Start Server
	// ================================================================

	logger.Info("===========================================")
	logger.Info("  Mana Telegram Clone")
	logger.Info("  http://%s:%d", cfg.Host, cfg.Port)
	logger.Info("===========================================")

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.WithComponent("server").Info("Listening on %s", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		logger.WithComponent("server").Error("Fatal: %v", err)
		os.Exit(1)
	case sig := <-quit:
		logger.WithComponent("server").Info("Received %v, shutting down...", sig)
		_ = srv.Close()
	}

	logger.WithComponent("server").Info("Server stopped.")
}
