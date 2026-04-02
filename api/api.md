# Mana API Guide

This document is the detailed usage guide for Mana as a framework.

It explains:

- what Mana provides
- how to start a Mana app
- how clients should talk to it
- how to use messaging, rooms, signaling, notifications, auth, and storage
- how to run it more safely in production
- what is available today and what is still partial

Mana is a framework for real-time communication systems in Go. It gives you the lower-level real-time backend pieces so you can build your own chat app, collaboration product, calling app, or WhatsApp-like MVP.

## 1. What Mana Is

Mana provides these framework capabilities:

- HTTP server and WebSocket entrypoint
- WebSocket messaging
- room membership and fanout
- user session tracking
- per-user multi-session handling
- signaling hub for RTC-style flows
- WebRTC manager integration
- message persistence foundation
- reconnect sync foundation
- notification fanout
- auth, RBAC, rate limiting, and origin controls
- metrics, logging, health, and graceful shutdown

Mana does not try to be a full finished product. You still build your own application logic on top of it.

## 2. Core Concepts

The easiest way to understand Mana is through its core objects.

### `mana.App`

`App` is the main framework instance. It wires together:

- WebSocket handling
- signaling
- rooms
- RTC
- notifications
- storage
- auth
- metrics

You create one with `mana.New(core.Config{...})`.

### `core.Config`

`core.Config` is the main configuration structure for a Mana app.

Important fields:

- `Host`
- `Port`
- `EnableRTC`
- `EnableE2EE`
- `EnableAuth`
- `JWTSecret`
- `JWTIssuer`
- `JWTExpiry`
- `AllowedOrigins`
- `RateLimitPerSecond`
- `RateLimitBurst`
- `MaxMessageSize`
- `EnableTLS`
- `TLSCertFile`
- `TLSKeyFile`
- `ReadTimeout`
- `WriteTimeout`
- `IdleTimeout`
- `GracefulShutdownTimeout`
- `STUNServers`
- `MessageStorePath`

Defined in [config.go](../core/config.go).

### `core.Message`

`core.Message` is the framework-level stored/message event object for actual messaging payloads.

Fields:

- `ID`
- `Type`
- `RoomID`
- `SenderID`
- `TargetID`
- `Payload`
- `Timestamp`
- `AckID`

Defined in [types.go](../core/types.go).

### `core.Signal`

`core.Signal` is the transport object used over WebSocket for signaling and framework events.

Fields:

- `Type`
- `From`
- `To`
- `RoomID`
- `Payload`
- `SDP`
- `Candidate`
- `Ready`
- `AckID`
- `Timestamp`

Defined in [types.go](../core/types.go).

### Rooms

Rooms are the framework’s live fanout units.

Use them for:

- group chat
- 1:1 DM rooms
- live collaboration rooms
- RTC call rooms

Room state lives under [room](../room).

### Signal Hub

The signaling hub tracks peers, rooms, direct sends, and room broadcasts.

It lives under [signaling](../signaling).

### Notification Hub

The notification hub lets you send direct alerts to a user, even when the user is not in the currently active room.

It lives under [notification](../notification).

### Message Store

The message store is the persistence foundation used for offline sync and reconnect replay.

It lives under [storage](../storage).

## 3. Recommended Starting Pattern

The normal server setup flow is:

1. create a config
2. create `mana.App`
3. register event hooks
4. register your own HTTP endpoints on `app.Mux()`
5. start the app

Example:

```go
package main

import (
	"log"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	cfg := core.DefaultConfig()
	cfg.Host = "0.0.0.0"
	cfg.Port = 8080
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.EnableAuth = false
	cfg.AllowedOrigins = []string{"http://localhost:3000"}
	cfg.MessageStorePath = "./data/messages.json"
	cfg.ReadTimeout = 15 * time.Second
	cfg.WriteTimeout = 15 * time.Second
	cfg.IdleTimeout = 60 * time.Second
	cfg.GracefulShutdownTimeout = 15 * time.Second

	app := mana.New(cfg)

	app.OnMessage(func(msg core.Message) {
		log.Printf("room=%s sender=%s size=%d", msg.RoomID, msg.SenderID, len(msg.Payload))
	})

	log.Fatal(app.Start())
}
```

Main framework file: [app.go](../app.go).

## 4. Config Guide

### Minimal development config

For local development:

```go
cfg := core.DefaultConfig()
cfg.Host = "localhost"
cfg.Port = 8080
cfg.EnableRTC = true
cfg.EnableE2EE = true
cfg.EnableAuth = false
cfg.AllowedOrigins = []string{"*"}
```

This is convenient, but not how you should run public production traffic.

### Recommended production-oriented config

```go
cfg := core.DefaultConfig()
cfg.Host = "0.0.0.0"
cfg.Port = 8443
cfg.EnableRTC = true
cfg.EnableE2EE = true
cfg.EnableAuth = true
cfg.JWTSecret = "replace-with-a-real-secret-at-least-32-bytes"
cfg.JWTIssuer = "your-app"
cfg.JWTExpiry = 24 * time.Hour
cfg.AllowedOrigins = []string{
	"https://app.example.com",
	"https://admin.example.com",
}
cfg.MaxMessageSize = 1 << 20
cfg.RateLimitPerSecond = 100
cfg.RateLimitBurst = 200
cfg.EnableTLS = true
cfg.TLSCertFile = "/path/to/fullchain.pem"
cfg.TLSKeyFile = "/path/to/privkey.pem"
cfg.MessageStorePath = "/var/lib/mana/messages.json"
```

### Notes on important config fields

`AllowedOrigins`

- if empty, all origins are rejected
- use exact frontend origins in production
- avoid `[]string{"*"}` in public deployments

`JWTSecret`

- only matters when `EnableAuth` is `true`
- should be at least 32 bytes
- treat it like a production secret, not source-controlled text

`MaxMessageSize`

- protects the server from oversized payloads
- raise it only if your app truly needs it

`RateLimitPerSecond` and `RateLimitBurst`

- protect the server from floods
- tune these based on your app’s real usage

`MessageStorePath`

- enables file-backed persistence
- if unset, storage falls back to in-memory behavior

`EnableTLS`

- use this if Mana terminates TLS directly
- if TLS is terminated at a reverse proxy, configure the proxy carefully instead

## 5. Public App API

These are the main public entry points on `App`.

### Construction

```go
app := mana.New(cfg)
```

### Event hooks

```go
app.OnMessage(func(msg core.Message) {})
app.OnUserJoin(func(roomID string, user core.User) {})
app.OnUserLeave(func(roomID string, user core.User) {})
app.OnCallStart(func(evt core.CallEvent) {})
app.OnCallEnd(func(evt core.CallEvent) {})
app.OnSignal(core.SignalTyping, func(sig core.Signal) {})
```

Hook methods are defined in [app.go](../app.go).

### Component accessors

```go
app.RoomManager()
app.SignalHub()
app.RTCManager()
app.JWTAuth()
app.RBAC()
app.Metrics()
app.Mux()
app.KeyExchange()
app.CallManager()
app.Logger()
app.NotificationHub()
app.MessageStore()
```

These let you build your own application logic on top of the framework.

### Lifecycle

```go
app.Start()
app.StartWithGracefulShutdown()
app.Shutdown(ctx)
```

## 6. HTTP Endpoints Exposed By Mana

Mana itself registers these framework endpoints when started:

- `/ws`
- `/health`
- `/metrics`

### `/ws`

This is the main WebSocket endpoint.

Clients connect here to:

- send messages
- join rooms
- send typing events
- exchange RTC signaling
- receive presence/notification/broadcast events

### `/health`

Basic health and runtime status endpoint.

Useful for:

- liveness checks
- readiness checks
- container health probes

### `/metrics`

Prometheus-style metrics endpoint.

Useful for:

- dashboards
- alerting
- capacity planning

You can also add your own routes using:

```go
mux := app.Mux()
mux.HandleFunc("/api/...", yourHandler)
```

## 7. WebSocket Client Contract

Clients usually connect like this:

```text
ws://host:port/ws
```

or with auth:

```text
ws://host:port/ws?token=JWT_HERE
```

or with auth plus device tracking:

```text
ws://host:port/ws?token=JWT_HERE&device_id=device-1
```

### Why `device_id` matters

If you pass `device_id`, Mana creates a session ID like:

```text
userID::deviceID
```

That helps the framework support:

- multiple tabs
- multiple devices
- reconnect sync per device
- fanout to all active sessions of the same user

The WebSocket handler behavior is implemented in [handler.go](../ws/handler.go).

## 8. Signal Types

Common signal types currently recognized:

- `join`
- `leave`
- `message`
- `typing`
- `offer`
- `answer`
- `candidate`
- `key_exchange`
- `call_start`
- `call_end`
- `message_sync`
- `ice_restart`
- `mute`
- `camera_toggle`
- `screen_share_start`
- `screen_share_stop`
- `pin`
- `error`

Constants are defined in [types.go](../core/types.go).

## 9. Messaging Guide

### Sending a room message

Client sends:

```json
{
  "type": "message",
  "room_id": "room-1",
  "payload": [72, 101, 108, 108, 111],
  "ack_id": "ack-1"
}
```

Notes:

- `payload` is raw bytes serialized as JSON array data
- if `ack_id` is present, Mana responds immediately with an `ack`
- the framework stamps sender identity server-side

### Receiving ack

Client may receive:

```json
{
  "type": "ack",
  "ack_id": "ack-1"
}
```

### Server-side message handling

Register:

```go
app.OnMessage(func(msg core.Message) {
	// business logic here
})
```

By the time your handler runs:

- sender identity is already set
- message persistence may already have happened
- recipients may already be derived from room membership

Framework message persistence path is in [app.go](../app.go).

### Direct messaging

Direct messages use `TargetID` or `Signal.To`.

Example signal:

```json
{
  "type": "message",
  "to": "user-b",
  "payload": [72, 105]
}
```

If the target user has multiple live sessions, Mana can fan out to all of them.

## 10. Room Guide

### Creating rooms

Use the room manager directly:

```go
app.RoomManager().Create("room-1", "General")
```

### Joining rooms

Clients usually join by sending:

```json
{
  "type": "join",
  "room_id": "room-1"
}
```

The router:

- joins the session to the room
- adds the peer to signal-hub room membership
- broadcasts room presence

### Leaving rooms

Client sends:

```json
{
  "type": "leave",
  "room_id": "room-1"
}
```

### Listing rooms

```go
rooms := app.RoomManager().List()
```

Room/session code lives under [room](../room).

## 11. Presence Guide

Mana supports presence in two practical ways.

### Room-based presence

When a peer joins or leaves a room, the router broadcasts a `presence` event with:

- `type`
- `user_id`
- `username`
- `room_id`
- `online`

This is useful for chat rooms, live channels, and call rooms.

### App-wide online awareness

For app-wide online/offline UI, use user session tracking in your own app logic.

Typical pattern:

- query connected sessions from the hub
- expose your own `/api/online`
- update the frontend from that endpoint or custom signal fanout

That is how the Telegram clone in this repo handles global online badges.

## 12. Typing Indicators

Client sends:

```json
{
  "type": "typing",
  "room_id": "room-1"
}
```

Mana broadcasts the typing event to other peers in the room.

This is meant for live UI hints, not durable state.

## 13. Notifications

The notification hub is for direct user-targeted alerts outside the room broadcast path.

Example:

```go
ctx := context.Background()
app.NotificationHub().Send(ctx, "alice", core.Notification{
	ID:    "notif-1",
	Type:  "notification",
	Title: "New Alert",
	Body:  "Background processing finished",
	Data: map[string]interface{}{
		"job_id": "42",
	},
})
```

This is useful for:

- background task alerts
- delivery status updates
- read receipts
- sync-complete messages
- global app notifications

Notification type is defined in [types.go](../core/types.go).

## 14. Auth Guide

If `EnableAuth` is true, WebSocket connections require a valid JWT.

### Creating a token

```go
token, err := app.JWTAuth().GenerateToken("user-id", "alice", "user")
```

### Connecting with token

Client connects with:

```text
/ws?token=JWT_HERE
```

### Auth behavior

When auth is enabled:

- invalid or missing token means connection rejected
- user ID, username, and role come from JWT claims
- framework identity should come from the server, not the client payload

Auth package lives under [auth](../auth).

## 15. RBAC Guide

Mana includes RBAC support and the router checks permissions for some framework actions.

Examples of guarded operations include:

- room join
- message send
- call start
- call end

Access RBAC with:

```go
rbac := app.RBAC()
```

If you need app-specific authorization behavior, combine framework RBAC with your own HTTP and signal validation logic.

## 16. E2EE Guide

Mana includes E2EE building blocks, not a complete WhatsApp-grade secure messaging protocol.

Current foundation includes:

- encryption helpers
- key exchange helpers
- X3DH-style identity/bundle flows

Access it with:

```go
kx := app.KeyExchange()
```

Use cases:

- publish and store public keys
- exchange key material out-of-band through your app endpoints
- support encrypted message payloads at the application layer

E2EE code lives under [e2ee](../e2ee).

Important boundary:

- Mana does not yet provide a full ratcheting session lifecycle equivalent to WhatsApp or Signal

## 17. WebRTC And Signaling Guide

Mana includes signaling infrastructure and RTC manager building blocks.

### Offer flow

Caller sends:

```json
{
  "type": "offer",
  "room_id": "call-room",
  "sdp": "..."
}
```

### Answer flow

Callee sends:

```json
{
  "type": "answer",
  "to": "peer-session-id",
  "room_id": "call-room",
  "sdp": "..."
}
```

### ICE candidate flow

```json
{
  "type": "candidate",
  "room_id": "call-room",
  "candidate": { "...": "..." }
}
```

### Call lifecycle

You can also observe call lifecycle events with:

```go
app.OnCallStart(func(evt core.CallEvent) {})
app.OnCallEnd(func(evt core.CallEvent) {})
```

### RTC notes

What Mana provides now:

- signaling transport
- RTC manager
- call manager
- ICE restart support
- SFU/media-oriented pieces in the repo

What is still partial:

- TURN-first production hardening
- network switch recovery
- complete product-grade call orchestration

RTC code lives under [rtc](../rtc).

## 18. Offline Sync And Durable Messaging

If `MessageStorePath` is set, Mana uses a file-backed message store.

Current capabilities:

- store messages
- track delivery status
- replay historical messages on reconnect
- sync batches per user/device

Access:

```go
store := app.MessageStore()
```

This is a foundation for:

- offline delivery
- reconnect sync
- message continuity across sessions

Important limitation:

- this is not yet a full product-complete offline sync platform with all cursoring, retention, and conflict-handling behavior

Storage code lives under [storage](../storage).

## 19. Multi-Device Behavior

Mana now supports multi-session users.

That means one logical user can have:

- multiple browser tabs
- multiple devices
- multiple active sessions

Current behavior:

- session IDs can include `userID::deviceID`
- direct fanout can reach all active sessions of a user
- reconnect sync can be device-aware
- room membership can track multiple sessions per user

Best practice:

- always pass a stable `device_id` from the client
- do not trust client-declared `from`
- let the server derive identity from auth/session state

## 20. Custom App Routes

Mana is meant to be extended with your own HTTP endpoints.

Example:

```go
app := mana.New(cfg)
app.Mux().HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
})
```

This is how you build:

- login/register flows
- contact lists
- message history APIs
- admin endpoints
- public-key publishing endpoints

The Telegram clone under [tmp telegram-clone](../tmp%20telegram-clone) is the best full example of this pattern in the repo.

## 21. Logging And Metrics

Access:

```go
logger := app.Logger()
metrics := app.Metrics()
```

Use cases:

- structured logs
- request/system logging
- room/peer/call counters
- dashboards and alerting

Observability code lives under [observ](../observ).

## 22. Production Deployment Guidance

Mana can be used in production for controlled workloads, but you should deploy it carefully.

### Recommended production checklist

1. Enable auth.
2. Use a real 32+ byte JWT secret.
3. Set explicit `AllowedOrigins`.
4. Set message size limits.
5. Keep rate limiting enabled.
6. Use TLS or place Mana behind a trusted reverse proxy.
7. Persist messages with `MessageStorePath` if you need reconnect continuity.
8. Use graceful shutdown in your process lifecycle.
9. Scrape `/metrics`.
10. Put health probes on `/health`.

### Reverse proxy notes

If using nginx, Caddy, or another proxy:

- forward WebSocket upgrades correctly
- preserve real client IP where needed
- terminate TLS safely
- apply outer request/body protections too

### Scaling notes

Mana is still strongest in single-node or controlled deployments.

If you need:

- cross-node room state
- global pub-sub
- distributed message routing

you will need extra infrastructure beyond the current framework.

## 23. Security Guidance

### Good current practices

- server-owned identity from JWT/session state
- explicit origin controls
- message size limits
- rate limiting
- TLS support
- room/session encapsulation
- device-aware session tracking

### Things you should still treat carefully

- do not rely on the client’s `from` field as truth
- do not expose wildcard origins in public production
- do not store weak JWT secrets
- do not market current E2EE as WhatsApp-grade
- do not assume the current file-backed store is enough for high-scale durable messaging

## 24. Client-Side Best Practices

If you are building a browser or mobile client on Mana:

1. connect with a JWT when auth is enabled
2. pass a stable `device_id`
3. let the server own identity
4. use `ack_id` for optimistic messaging UX
5. join rooms before sending room-bound events
6. treat `message_sync` as reconnect history, not just live traffic
7. use notification events for UI alerts that are not room-bound

## 25. Practical Usage Patterns

### Pattern A: simple group chat

- create room in your app code
- clients connect to `/ws`
- clients send `join`
- clients send `message`
- server handles `OnMessage`

### Pattern B: WhatsApp-like MVP

- use auth
- issue JWT on login
- give each device a stable `device_id`
- create DM rooms from your own HTTP API
- use `NotificationHub` for delivery/read alerts
- persist messages with `MessageStorePath`
- replay sync on reconnect

### Pattern C: RTC signaling backend

- enable RTC
- use room join
- exchange `offer`, `answer`, and `candidate`
- use `OnCallStart` and `OnCallEnd` for app-level lifecycle

## 26. Current Limitations You Should Design Around

- distributed scaling is not complete
- tracing is not complete
- multi-device is partial, not fully product-complete
- offline sync is partial, not fully product-complete
- E2EE is a foundation, not a finished secure messaging stack
- RTC hardening still needs more work for demanding public production use

## 27. Best Repo Examples

Use these as reference points:

- [cmd/example/main.go](../cmd/example/main.go)
- [examples/full/main.go](../examples/full/main.go)
- [tmp telegram-clone/main.go](../tmp%20telegram-clone/main.go)
- [tmp telegram-clone/frontend/index.html](../tmp%20telegram-clone/frontend/index.html)

## 28. Summary

Mana is best understood as:

- a real-time communication framework
- strong for MVPs and controlled production use
- extensible through your own app routes and business logic
- already useful for chat/signaling systems
- still growing toward more complete durable messaging, multi-device, and hardened production infrastructure

If you use it as a framework foundation instead of expecting a fully finished product backend, it gives you a lot of real value today.

## 29. Implementation Recipes

This section is the practical part of the guide.

If you are trying to actually build with Mana, start here.

### Recipe 1: Smallest Possible Server

This is the smallest useful Mana server:

```go
package main

import (
	"log"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	cfg := core.DefaultConfig()
	cfg.Host = "localhost"
	cfg.Port = 8080
	cfg.EnableRTC = false
	cfg.EnableE2EE = false
	cfg.EnableAuth = false
	cfg.AllowedOrigins = []string{"*"}

	app := mana.New(cfg)

	app.OnMessage(func(msg core.Message) {
		log.Printf("message from %s in %s", msg.SenderID, msg.RoomID)
	})

	log.Fatal(app.Start())
}
```

What this gives you:

- `/ws`
- `/health`
- `/metrics`
- message handling
- room join/leave signaling

### Recipe 2: Proper App Bootstrap

This is a better starting pattern for a real project:

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	cfg := core.DefaultConfig()
	cfg.Host = "0.0.0.0"
	cfg.Port = 8080
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.EnableAuth = false
	cfg.AllowedOrigins = []string{"http://localhost:3000"}
	cfg.MessageStorePath = "./data/messages.json"

	app := mana.New(cfg)
	mux := app.Mux()

	app.OnMessage(func(msg core.Message) {
		log.Printf("received room=%s from=%s bytes=%d", msg.RoomID, msg.SenderID, len(msg.Payload))
	})

	app.OnUserJoin(func(roomID string, user core.User) {
		log.Printf("%s joined %s", user.Username, roomID)
	})

	app.OnUserLeave(func(roomID string, user core.User) {
		log.Printf("%s left %s", user.Username, roomID)
	})

	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	log.Fatal(app.Start())
}
```

### Recipe 3: Add JWT Auth

If you want logged-in users, this is the common pattern:

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/auth"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	cfg := core.DefaultConfig()
	cfg.EnableAuth = true
	cfg.JWTSecret = "replace-this-with-a-real-secret-at-least-32-bytes"
	cfg.JWTIssuer = "my-chat-app"
	cfg.JWTExpiry = 24 * time.Hour
	cfg.AllowedOrigins = []string{"http://localhost:3000"}

	app := mana.New(cfg)
	mux := app.Mux()

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			UserID   string `json:"user_id"`
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		token, err := app.JWTAuth().GenerateToken(req.UserID, req.Username, auth.RoleUser)
		if err != nil {
			http.Error(w, "token error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	})

	log.Fatal(app.Start())
}
```

Frontend connection example:

```js
const ws = new WebSocket(
  `ws://localhost:8080/ws?token=${encodeURIComponent(token)}&device_id=web-1`
);
```

### Recipe 4: Build a Simple Room Chat

This pattern is good for a Slack- or Discord-like room.

Server:

```go
package main

import (
	"log"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	cfg := core.DefaultConfig()
	cfg.AllowedOrigins = []string{"*"}

	app := mana.New(cfg)
	app.RoomManager().Create("general", "General")

	app.OnMessage(func(msg core.Message) {
		log.Printf("chat message in %s from %s: %s", msg.RoomID, msg.SenderID, string(msg.Payload))
	})

	log.Fatal(app.Start())
}
```

Client:

```js
const ws = new WebSocket("ws://localhost:8080/ws?user_id=alice&username=alice");

ws.onopen = () => {
  ws.send(JSON.stringify({ type: "join", room_id: "general" }));
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  console.log("incoming", msg);
};

function sendMessage(text) {
  ws.send(
    JSON.stringify({
      type: "message",
      room_id: "general",
      payload: Array.from(new TextEncoder().encode(text)),
      ack_id: crypto.randomUUID(),
    })
  );
}
```

### Recipe 5: Build 1:1 DMs

Mana does not force a DM product model on you. The usual pattern is:

1. your app creates a stable room ID for two users
2. your app creates that room
3. both users join that room
4. all messages use that room ID

Example:

```go
func dmRoomID(a, b string) string {
	if a < b {
		return "dm:" + a + ":" + b
	}
	return "dm:" + b + ":" + a
}

func ensureDM(app *mana.App, userA, userB string) string {
	roomID := dmRoomID(userA, userB)
	app.RoomManager().Create(roomID, roomID)
	return roomID
}
```

Example HTTP endpoint:

```go
mux.HandleFunc("/api/dm", func(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserA string `json:"user_a"`
		UserB string `json:"user_b"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	roomID := ensureDM(app, req.UserA, req.UserB)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"room_id": roomID,
	})
})
```

Client then sends:

```json
{
  "type": "join",
  "room_id": "dm:alice:bob"
}
```

### Recipe 6: Presence UI

Room presence is automatic when users join and leave rooms. If you want a contact-list style online/offline indicator, expose an endpoint based on active sessions.

Example:

```go
mux.HandleFunc("/api/online", func(w http.ResponseWriter, r *http.Request) {
	users := []string{"alice", "bob", "charlie"}
	var online []string

	for _, userID := range users {
		if len(app.SignalHub().UserPeerIDs(userID)) > 0 {
			online = append(online, userID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(online)
})
```

Client:

```js
async function refreshOnline() {
  const res = await fetch("/api/online");
  const onlineUsers = await res.json();
  console.log("online:", onlineUsers);
}
```

### Recipe 7: Typing Indicator

Client send:

```js
ws.send(JSON.stringify({
  type: "typing",
  room_id: "general"
}));
```

Client receive:

```js
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "typing") {
    console.log(`${msg.from} is typing`);
  }
};
```

### Recipe 8: Delivery Acks In The UI

If the client sends an `ack_id`, Mana immediately returns an `ack`.

Send:

```js
const ackID = crypto.randomUUID();

ws.send(JSON.stringify({
  type: "message",
  room_id: "general",
  payload: Array.from(new TextEncoder().encode("hello")),
  ack_id: ackID
}));
```

Receive:

```js
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "ack" && msg.ack_id === ackID) {
    console.log("server received message");
  }
};
```

### Recipe 9: User Notifications

Use notifications for non-room alerts.

Server:

```go
ctx := context.Background()

err := app.NotificationHub().Send(ctx, "alice", core.Notification{
	ID:    "notification-1",
	Type:  "notification",
	Title: "Upload finished",
	Body:  "Your export is ready",
	Data: map[string]interface{}{
		"job_id": "job-42",
	},
})
if err != nil {
	log.Printf("notification error: %v", err)
}
```

Client:

```js
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "notification") {
    console.log("notification", msg.title, msg.body, msg.data);
  }
};
```

### Recipe 10: RTC Signaling

Mana can carry WebRTC signaling for you.

Offer sender:

```js
const pc = new RTCPeerConnection({
  iceServers: [{ urls: "stun:stun.l.google.com:19302" }]
});

pc.onicecandidate = (event) => {
  if (!event.candidate) return;
  ws.send(JSON.stringify({
    type: "candidate",
    room_id: "call-room",
    candidate: event.candidate
  }));
};

const offer = await pc.createOffer();
await pc.setLocalDescription(offer);

ws.send(JSON.stringify({
  type: "offer",
  room_id: "call-room",
  sdp: offer.sdp
}));
```

Offer receiver:

```js
async function handleOffer(msg) {
  const pc = new RTCPeerConnection({
    iceServers: [{ urls: "stun:stun.l.google.com:19302" }]
  });

  await pc.setRemoteDescription({
    type: "offer",
    sdp: msg.sdp
  });

  const answer = await pc.createAnswer();
  await pc.setLocalDescription(answer);

  ws.send(JSON.stringify({
    type: "answer",
    to: msg.from,
    room_id: msg.room_id,
    sdp: answer.sdp
  }));
}
```

Dispatcher:

```js
ws.onmessage = async (event) => {
  const msg = JSON.parse(event.data);

  if (msg.type === "offer") {
    await handleOffer(msg);
  }

  if (msg.type === "answer") {
    await pc.setRemoteDescription({
      type: "answer",
      sdp: msg.sdp
    });
  }

  if (msg.type === "candidate") {
    await pc.addIceCandidate(msg.candidate);
  }
};
```

### Recipe 11: Offline Sync

Turn on durable storage:

```go
cfg := core.DefaultConfig()
cfg.MessageStorePath = "./data/messages.json"
```

When a device reconnects, Mana can send a `message_sync` batch.

Client-side:

```js
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);

  if (msg.type === "message_sync") {
    for (const historical of msg.messages) {
      console.log("historical message", historical);
    }
  }
};
```

### Recipe 12: Multi-Device Connection

Use a stable client-side device ID.

Browser example:

```js
function getDeviceID() {
  const key = "my-chat-device-id";
  let id = localStorage.getItem(key);
  if (!id) {
    id = crypto.randomUUID();
    localStorage.setItem(key, id);
  }
  return id;
}

const deviceID = getDeviceID();
const ws = new WebSocket(
  `/ws?token=${encodeURIComponent(token)}&device_id=${encodeURIComponent(deviceID)}`
);
```

Why this matters:

- reconnect sync is device-aware
- a user can have multiple active sessions
- direct sends can reach all active sessions

### Recipe 13: Your Own REST API On Top Of Mana

Mana is intended to sit under your app routes.

Example chat-room creation endpoint:

```go
type CreateRoomRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	room, err := app.RoomManager().Create(req.ID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(room)
})
```

### Recipe 14: Production Startup

This is a more realistic production-oriented skeleton:

```go
package main

import (
	"log"
	"os"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	cfg := core.DefaultConfig()
	cfg.Host = "0.0.0.0"
	cfg.Port = 8443
	cfg.EnableAuth = true
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.JWTSecret = os.Getenv("MANA_JWT_SECRET")
	cfg.JWTIssuer = "my-prod-app"
	cfg.JWTExpiry = 24 * time.Hour
	cfg.AllowedOrigins = []string{
		"https://app.example.com",
	}
	cfg.MaxMessageSize = 1 << 20
	cfg.RateLimitPerSecond = 100
	cfg.RateLimitBurst = 200
	cfg.MessageStorePath = "/var/lib/mana/messages.json"
	cfg.ReadTimeout = 15 * time.Second
	cfg.WriteTimeout = 15 * time.Second
	cfg.IdleTimeout = 60 * time.Second
	cfg.GracefulShutdownTimeout = 15 * time.Second

	app := mana.New(cfg)

	if err := app.StartWithGracefulShutdown(); err != nil {
		log.Fatal(err)
	}
}
```

### Recipe 15: A Better Project Structure

For a real app built on Mana, a good structure is usually:

```text
myapp/
├── cmd/server/main.go
├── internal/httpapi/
├── internal/chat/
├── internal/auth/
├── internal/store/
├── internal/rtc/
├── web/
└── configs/
```

And then:

- let Mana handle transport and framework plumbing
- keep your product logic in your own packages
- use `app.Mux()` for your app endpoints
- use `OnMessage` and `OnSignal` for domain behavior

## 30. End-To-End Example

This is the mental model for a simple WhatsApp-like MVP on Mana.

### Server side

```go
cfg := core.DefaultConfig()
cfg.EnableAuth = true
cfg.EnableRTC = true
cfg.EnableE2EE = true
cfg.MessageStorePath = "./data/messages.json"

app := mana.New(cfg)
mux := app.Mux()

// login endpoint
// contacts endpoint
// dm creation endpoint
// history endpoint
// online endpoint

app.OnMessage(func(msg core.Message) {
	// optional business logic
})

app.OnCallStart(func(evt core.CallEvent) {
	// optional analytics or auditing
})

log.Fatal(app.Start())
```

### Frontend side

```js
const token = await loginAndGetJWT();
const deviceID = getOrCreateDeviceID();

const ws = new WebSocket(
  `/ws?token=${encodeURIComponent(token)}&device_id=${encodeURIComponent(deviceID)}`
);

ws.onopen = () => {
  ws.send(JSON.stringify({ type: "sync" }));
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);

  if (msg.type === "message") {
    renderIncomingMessage(msg);
  }

  if (msg.type === "message_sync") {
    replayHistoricalMessages(msg.messages);
  }

  if (msg.type === "notification") {
    renderNotification(msg);
  }
};

function joinRoom(roomID) {
  ws.send(JSON.stringify({ type: "join", room_id: roomID }));
}

function sendText(roomID, text) {
  ws.send(JSON.stringify({
    type: "message",
    room_id: roomID,
    payload: Array.from(new TextEncoder().encode(text)),
    ack_id: crypto.randomUUID()
  }));
}
```

## 31. Final Guidance

If you want to use Mana well:

1. treat it as a framework foundation
2. keep your product-specific logic in your own handlers and endpoints
3. use auth and stable `device_id` values early
4. let the server own identity
5. enable persistence if you want reconnect continuity
6. start simple, then layer your product model on top

That is the most reliable way to build something real with it.

## 32. Developer Deep Dive

This section is the missing piece for developers who want to understand not just what to call, but how the framework behaves internally and where their own code fits.

### The division of responsibility

When building on Mana, think in two layers.

Framework layer:

- accepts WebSocket connections
- authenticates users
- tracks sessions and devices
- handles rooms and signaling
- handles ack responses
- handles reconnect sync foundation
- exposes metrics and health

Application layer:

- owns users, contacts, groups, and policies
- decides which users are allowed to talk
- decides how DM rooms are named
- exposes REST APIs for product state
- decides what to store beyond the framework message store
- implements frontend behavior

If you keep that boundary clear, the framework becomes much easier to use.

### What happens when a socket connects

The connection flow is:

1. the client connects to `/ws`
2. the WebSocket handler reads auth and `device_id`
3. a session ID is created
4. `App` registers the peer into the signaling hub and session maps
5. the notification hub registers the connection
6. reconnect sync may be replayed

That means your frontend should almost always connect with:

- a valid token when auth is enabled
- a stable `device_id`

Browser example:

```js
function getOrCreateDeviceID() {
  const key = "mana-device-id";
  let id = localStorage.getItem(key);
  if (!id) {
    id = crypto.randomUUID();
    localStorage.setItem(key, id);
  }
  return id;
}

const deviceID = getOrCreateDeviceID();
const ws = new WebSocket(
  `ws://localhost:8080/ws?token=${encodeURIComponent(token)}&device_id=${encodeURIComponent(deviceID)}`
);
```

### What happens when a message arrives

When the client sends:

```json
{
  "type": "message",
  "room_id": "general",
  "payload": [72, 101, 108, 108, 111],
  "ack_id": "ack-1"
}
```

the framework path is roughly:

1. WebSocket handler reads raw bytes
2. `App` detects `ack_id` and sends an immediate ack
3. the signaling router parses the signal
4. the router converts the room message into a `core.Message`
5. the app derives recipients
6. the app persists the message in the message store
7. the app marks online recipients as delivered
8. your `OnMessage` hook runs
9. room or direct fanout happens through the hub/router path

That is why `OnMessage` is a good place for business logic, but not the only work happening.

### The most important design rule

Do not architect your app around trusting the client-supplied `from`.

Instead:

- trust JWT identity
- trust session identity
- let the framework stamp sender identity

That keeps your app aligned with Mana’s newer session model and multi-device behavior.

## 33. End-To-End Build Walkthrough

This section shows how a developer would actually build a small project on Mana.

### Step 1: create the app

```go
package main

import (
	"log"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	cfg := core.DefaultConfig()
	cfg.Host = "0.0.0.0"
	cfg.Port = 8080
	cfg.EnableAuth = true
	cfg.EnableRTC = true
	cfg.EnableE2EE = true
	cfg.JWTSecret = "replace-with-a-real-secret-at-least-32-bytes"
	cfg.JWTIssuer = "myapp"
	cfg.JWTExpiry = 24 * time.Hour
	cfg.AllowedOrigins = []string{"http://localhost:3000"}
	cfg.MessageStorePath = "./data/messages.json"

	app := mana.New(cfg)

	app.OnMessage(func(msg core.Message) {
		log.Printf("message sender=%s room=%s", msg.SenderID, msg.RoomID)
	})

	log.Fatal(app.Start())
}
```

### Step 2: add login

```go
mux := app.Mux()

mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	token, err := app.JWTAuth().GenerateToken(req.UserID, req.Username, "user")
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
})
```

### Step 3: add room creation

```go
mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	room, err := app.RoomManager().Create(req.ID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(room)
})
```

### Step 4: connect from the frontend

```js
const loginRes = await fetch("/api/login", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    user_id: "alice",
    username: "alice"
  })
});

const { token } = await loginRes.json();
const deviceID = getOrCreateDeviceID();

const ws = new WebSocket(
  `ws://localhost:8080/ws?token=${encodeURIComponent(token)}&device_id=${encodeURIComponent(deviceID)}`
);
```

### Step 5: join a room

```js
ws.onopen = () => {
  ws.send(JSON.stringify({
    type: "join",
    room_id: "general"
  }));
};
```

### Step 6: send a message

```js
function sendMessage(text) {
  ws.send(JSON.stringify({
    type: "message",
    room_id: "general",
    payload: Array.from(new TextEncoder().encode(text)),
    ack_id: crypto.randomUUID()
  }));
}
```

### Step 7: receive messages and notifications

```js
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);

  if (msg.type === "ack") {
    console.log("message accepted", msg.ack_id);
  }

  if (msg.type === "message") {
    console.log("incoming room message", msg);
  }

  if (msg.type === "presence") {
    console.log("presence update", msg);
  }

  if (msg.type === "notification") {
    console.log("notification", msg);
  }

  if (msg.type === "message_sync") {
    console.log("historical replay", msg.messages);
  }
};
```

That is the simplest real project shape on top of Mana.

## 34. Project Patterns By Use Case

### Pattern: simple chat room app

Best fit:

- team chat
- class project
- internal channel app

Recommended setup:

- use `RoomManager().Create(...)`
- clients send `join`
- clients send `message`
- use `OnMessage`
- use room presence events

### Pattern: WhatsApp-like MVP

Best fit:

- direct chat
- group chat
- online/offline indicators
- reconnect continuity

Recommended setup:

- enable auth
- use stable `device_id`
- create deterministic DM rooms
- add `/api/online`
- enable `MessageStorePath`
- use notifications for read/delivery-like UX

DM helper:

```go
func dmRoomID(a, b string) string {
	if a < b {
		return "dm:" + a + ":" + b
	}
	return "dm:" + b + ":" + a
}
```

### Pattern: RTC signaling backend

Best fit:

- browser calling experiments
- collaboration rooms
- product prototypes with audio/video

Recommended setup:

- enable RTC
- use room join first
- exchange `offer`, `answer`, `candidate`
- use `OnCallStart` and `OnCallEnd`

Offer sender example:

```js
const pc = new RTCPeerConnection({
  iceServers: [{ urls: "stun:stun.l.google.com:19302" }]
});

pc.onicecandidate = (event) => {
  if (!event.candidate) return;
  ws.send(JSON.stringify({
    type: "candidate",
    room_id: "call-room",
    candidate: event.candidate
  }));
};

const offer = await pc.createOffer();
await pc.setLocalDescription(offer);

ws.send(JSON.stringify({
  type: "offer",
  room_id: "call-room",
  sdp: offer.sdp
}));
```

## 35. Package-Level Usage Cheatsheet

### `mana`

Use for:

- app creation
- hooks
- lifecycle

Example:

```go
app := mana.New(cfg)
err := app.Start()
```

### `core`

Use for:

- config
- messages
- signals
- notifications
- call events

Example:

```go
cfg := core.DefaultConfig()
```

### `room`

Use for:

- room creation
- room listing
- room membership understanding

Example:

```go
_, _ = app.RoomManager().Create("general", "General")
```

### `signaling`

Usually accessed indirectly through `App`, but conceptually responsible for:

- direct send
- room broadcast
- peer tracking
- signal routing

### `notification`

Use for direct user alerts:

```go
_ = app.NotificationHub().Send(ctx, "alice", core.Notification{
	ID: "n1",
	Title: "Hello",
})
```

### `storage`

Use when you want durable messaging behavior:

```go
cfg.MessageStorePath = "./data/messages.json"
```

### `auth`

Use for login/token generation:

```go
token, _ := app.JWTAuth().GenerateToken("u1", "alice", "user")
```

## 36. Production Checklist With Explanation

### 1. Enable auth

Why:

- prevents anonymous public socket access
- gives the framework a trustworthy identity source

### 2. Use a real secret

Why:

- weak JWT secrets make auth meaningless

### 3. Set exact origins

Why:

- reduces cross-origin abuse risk

### 4. Keep rate limits and message size limits

Why:

- protects the server from abuse and accidental oversized traffic

### 5. Use `device_id`

Why:

- improves reconnect and multi-device behavior

### 6. Enable storage if continuity matters

Why:

- reconnecting clients can receive sync

### 7. Use health and metrics

Why:

- production visibility
- readiness/liveness checks

### 8. Use graceful shutdown

Why:

- cleaner restarts and fewer interrupted connections

## 37. Common Mistakes

### Mistake: trusting `from`

Wrong approach:

- treating client-provided `from` as the truth

Better approach:

- let server/session identity define the sender

### Mistake: building direct chat without deterministic room IDs

Wrong approach:

- creating random DM room IDs each time

Better approach:

- derive DM room IDs from both users consistently

### Mistake: no stable `device_id`

Wrong approach:

- reconnects always look like brand-new devices

Better approach:

- store a stable browser/mobile device ID

### Mistake: expecting the framework to replace all app APIs

Wrong approach:

- expecting Mana to provide your entire product backend

Better approach:

- use Mana for communication infrastructure and build app APIs around it

## 38. Final Developer Guidance

If you want to understand Mana well enough to implement a project:

1. read the architecture sections first
2. start from the minimal bootstrap example
3. add login
4. add room or DM creation APIs
5. connect with a stable `device_id`
6. build your frontend around `join`, `message`, `notification`, and `message_sync`
7. add RTC only after the messaging path is stable

Mana becomes much easier to work with once you stop treating it like a black box and instead see it as a real-time framework layer under your own product code.
