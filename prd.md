# 📄 Product Requirements Document (PRD)

## **Mana – Real-Time Communication Framework for Go**

---

# 1. 🧭 Overview

**Product Name:** Mana
**Type:** Backend Framework (Go)
**Category:** Real-Time Communication (RTC)

**Description:**
Mana is a developer-first Go framework that enables building real-time communication systems with minimal effort. It provides built-in support for:

* WebSocket-based messaging
* WebRTC (audio/video/screen sharing)
* Group and 1:1 communication
* End-to-End Encryption (E2EE)
* Scalable session and room management

**Vision:**

> To become the simplest and most powerful Go-native framework for building chat, voice, and video applications.

---

# 2. 🎯 Goals

## Primary Goals

* Provide a **plug-and-play RTC framework**
* Abstract complexity of WebRTC and signaling
* Enable rapid development of chat + call systems
* Maintain high performance and scalability

## Secondary Goals

* Developer-friendly API
* Modular architecture
* Open-source adoption

---

# 3. 👥 Target Users

* Backend developers (Go)
* Indie hackers / startups
* Teams building:

  * Chat apps
  * Video conferencing tools
  * Collaboration platforms
  * Gaming communication systems

---

# 4. 🚀 Core Features

## 4.1 Messaging (WebSocket Layer)

* Built on nhooyr.io/websocket
* Real-time messaging
* Group chat & 1:1 chat
* Message acknowledgments
* Typing indicators
* Reconnection handling
* Context-based connection management

---

## 4.2 WebRTC (Media Layer)

* Audio calls (1:1)
* Video calls (1:1)
* Group calls (multi-peer / SFU later)
* Screen sharing
* Media stream control

---

## 4.3 Signaling System

* Offer/Answer exchange
* ICE candidate handling
* Session negotiation via WebSocket

---

## 4.4 Room & Session Management

* Create/join/leave rooms
* Group session handling
* Peer tracking
* Presence (online/offline)

---

## 4.5 End-to-End Encryption (E2EE)

* Secure message encryption (AES-GCM)
* Key exchange system
* Future: double ratchet protocol

---

## 4.6 Developer API (Framework Layer)

### Example Usage:

```go
app := mana.New(mana.Config{
    Port:       8080,
    EnableRTC:  true,
    EnableE2EE: true,
})

app.OnMessage(func(msg mana.Message) {
    // handle message
})

app.Start()
```

---

## 4.7 Event System (Hooks)

* OnUserJoin
* OnUserLeave
* OnMessage
* OnCallStart
* OnCallEnd

---

## 4.8 Plugin System (Optional Future)

* Extend framework functionality
* Custom authentication
* Analytics plugins
* Moderation tools

---

# 4.9 WebSocket Abstraction Layer (NEW)

To ensure flexibility, performance, and long-term maintainability, Mana introduces a **WebSocket abstraction layer**.

## Design Goals

* Decouple framework from specific WebSocket implementations
* Allow pluggable backends (nhooyr, gobwas, future libs)
* Maintain stable public API
* Enable performance tuning without breaking apps

## Core Interface

```go
type Conn interface {
    Read(ctx context.Context) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
}
```

## Implementations

* nhooyr adapter (default, developer-friendly)
* gobwas adapter (optional, high-performance)

## Structure

```
mana/ws/
├── interface.go
├── nhooyr.go
├── gobwas.go (optional)
```

## Benefits

* Swap implementations without changing app code
* Optimize performance at scale
* Cleaner architecture

---

# 4.10 Notification System (NEW)

Mana provides a built-in semantic layer for sending notifications and alerts to individual users or groups of users independently of their active communication rooms.

## Key Features

* **Direct Delivery**: Send alerts directly to a User ID without knowing their room status.
* **Unified Interface**: Use `app.NotificationHub()` to deliver structured alerts (Title, Body, Data).
* **Real-Time Toasts**: Integrate with frontend to trigger UI components immediately.
* **Plug-and-Play**: Automatic registration of users upon WebSocket connection.

## Example Usage

```go
app.NotificationHub().Send(ctx, "alice", core.Notification{
    Title: "System Alert",
    Body:  "Your account has been verified.",
    Data:  map[string]interface{}{"status": "verified"},
})
```

---

# 5. 🧱 System Architecture

## Core Modules

```
mana/
├── ws/
├── signaling/
├── rtc/
├── media/
├── room/
├── call/
├── e2ee/
├── transport/
├── notification/
└── core/
```

---

## Architecture Flow

1. Client connects via WebSocket
2. Server manages session
3. WebRTC signaling exchanged via WS
4. Peer connections established
5. Media flows directly (P2P or SFU)

---

# 6. ⚙️ Tech Stack

* Go
* WebSocket: nhooyr.io/websocket (primary implementation)
* WebRTC (Pion)
* Redis / NATS
* golang.org/x/crypto
* STUN/TURN (coturn)

---

# 7. 🧪 Non-Functional Requirements

## Performance

* Low latency messaging (<100ms)

## Scalability

* Horizontal scaling

## Reliability

* Auto-reconnect

## Security

* E2EE

---

# 8. 📦 MVP Scope

## Phase 1

* WebSocket chat
* Signaling
* 1:1 audio call

## Phase 2

* Video calls
* Group chat

## Phase 3

* Screen sharing
* E2EE

## Phase 4

* SFU

---

# 9. 🧑‍💻 Developer Experience

```bash
mana init
mana run
```

---

# 14. 🏭 Production Requirements

## Authentication & Authorization

* JWT auth
* RBAC
* Session security

## Scalability

* Stateless nodes
* Redis/NATS pub-sub
* Load balancing

## Observability

* Logging
* Prometheus metrics
* OpenTelemetry tracing

## Reliability

* Retry strategies
* Session recovery
* Graceful shutdown

## WebRTC Handling

* ICE retry
* TURN fallback
* Network switch handling

## Security

* TLS
* Key lifecycle
* Rate limiting

## Deployment

* Docker
* Kubernetes

## Testing

* Unit + integration + load tests

---

# 🏁 Final Positioning

**Mana** is a production-ready real-time communication framework for Go.