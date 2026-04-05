# Mana Framework 🌀

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-AGPL--3.0-blue?style=flat-square)](LICENSE)
[![Status](https://img.shields.io/badge/Status-v0.3.0--Beta-orange?style=flat-square)](#current-production-position)

**Mana** is a batteries-included Go framework for building real-time communication applications with WebSocket messaging, rooms, WebRTC signaling, and **plug-and-play SQL batteries**. 

**Live Documentation & Examples**: [aswanidev-vs.github.io/Mana/](https://aswanidev-vs.github.io/Mana/)

It is designed for "Golden Path" project types:
*   **High-Fidelity Messaging**: Group chat, 1:1 DMs, and WhatsApp-like MVPs.
*   **Media Signaling**: Integrated SFU orchestration for high-performance audio/video calls.
*   **Shared Data Sovereignty**: Atomic relational persistence (SQLite / Postgres / MySQL).

**Current Status**: Mana is in an **active development stage (v0.3.0)**. While not yet 'production-grade' at the scale of global systems like Signal or WhatsApp, it provides a feature-complete and robust foundation for building working, scalable real-time applications and production MVPs.

---

## ⚡ Install

```bash
go get github.com/Aswanidev-vs/mana
```

## ⚡ Quick Start

A fully functional, persistent WebSocket messaging server in 15 lines:

```go
package main

import (
	"log"
	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	// 1. Initialize with SQL Batteries (Defaults to zero-config SQLite)
	app := mana.New(core.DefaultConfig())

	// 2. Add high-level event hooks
	app.OnMessage(func(msg core.Message) {
		log.Printf("[%s] %s → %s", msg.RoomID, msg.SenderID, string(msg.Payload))
	})

	// 3. Start high-performance engine
	log.Fatal(app.Start())
}
```

### Using SQL Batteries (Postgres Example)

```go
package main

import (
	"database/sql"
	"log"
	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/storage/manadb"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	db, _ := sql.Open("pgx", "postgres://user:pass@localhost:5432/mana")
	
	cfg := core.DefaultConfig()
	cfg.DatabaseTablePrefix = "app_"
	
	app := mana.New(cfg)
	
	// Inject your DB connection to enable persistent stores
	app.WithDatabase(db, manadb.Postgres)
	
	// Operations now share the same transaction pool
	app.OnMessage(func(msg core.Message) {
		// This message is automatically persisted to the SQL store
		log.Printf("Persisted message: %s", msg.ID)
	})

	log.Fatal(app.Start())
}
```

For a much more detailed implementation guide, including code examples for chat rooms, DMs, auth, notifications, RTC signaling, multi-device sessions, and production-minded setup, see [api/api.md](api/api.md).

---

## 🚀 Key Features & Differentiators

### 1. Nuclear Atomicity (`WithTx`)
Mana allows you to inject your existing `*sql.DB` pool and join shared transactions. You can wrap framework operations (like sending a receipt) and your own business logic (like updating a user's wallet) in a **single atomic transaction**. No more mismatched state between your app and the framework.

### 2. Advanced E2EE (X3DH + Double Ratchet)
WhatsApp-grade security with **X3DH** and **Double Ratchet** protocol (via Mellium integration). It provides an atomic, SQL-backed key store featuring forward secrecy and break-in recovery. **New**: Prekey consumption is now transactionally atomic to prevent race conditions in high-concurrency environments.

### 3. SFU-Oriented WebRTC
The signaling hub is built for scale, featuring integrated ICE candidate management, jitter buffering, congestion control, and NACK handling for resilient media. **New**: The RTC manager now features global resource cleanup (`Close()`) wired into the framework's graceful shutdown procedure.

### 4. Zero-Config SQL Persistence
Point Mana at a database DSN (PostgreSQL, MySQL, SQLite), and it will automatically handle table migrations and optimizations for messaging, identity, and social graphs. Includes a clean `WithTx` and `RunInTx` API for shared atomicity between framework and business logic.

### 5. Multi-Device Sync & Distribution
Device-aware connection tracking using standardized `::` session separators. Features cursor-based offline message replay, multi-session fanout, and stateless multi-node signal fanout using **Redis** or **NATS** backends.

---

## 🏗️ Project Architecture

Mana is modular by design, with core concerns separated into dedicated packages:

- [**`app.go`**](app.go): main framework entry point orchestration engine
- [**`core/`**](core/): shared types, config, and framework interfaces
- [**`ws/`**](ws/): high-performance WebSocket transport layer
- [**`signaling/`**](signaling/): signal routing and hub orchestration
- [**`room/`**](room/): room and session management
- [**`rtc/`**](rtc/): WebRTC connection and SFU-oriented pieces
- [**`e2ee/`**](e2ee/): encryption and key exchange helpers
- [**`notification/`**](notification/): user-targeted notifications
- [**`observ/`**](observ/): logging, metrics, OpenTelemetry tracing, and health helpers
- [**`storage/`**](storage/): message persistence and sync store including `storage/db` for SQL Batteries

---

## 📖 What You Can Build

Mana is currently a good fit for:

- simple WhatsApp-like chat apps
- private messaging apps
- group chat apps
- internal communication tools
- real-time prototypes with voice/video signaling

It is not yet a complete fit for:

- massive multi-region deployments
- full Signal/WhatsApp-grade E2EE lifecycle guarantees
- deeply hardened multi-node media infrastructure
- enterprise-grade observability and compliance requirements

## 🧪 Current Production Position

Mana is suitable today for:

- prototypes
- college projects
- hackathons
- startup MVPs
- internal or controlled production workloads

Use caution before calling it production-ready for broad public internet scale. The framework still needs more work in distributed scaling, tracing, advanced E2EE session lifecycle, and large-scale operational hardening.

## 🔒 Security Notes

Mana already includes a meaningful baseline:

- JWT auth support
- RBAC hooks
- origin controls
- rate limiting
- maximum message size enforcement
- TLS support
- Structured transport logging (replaces raw fmt.Printf)
- Atomic E2EE key consumption (transactional)
- Graceful connection and RTC resource cleanup
- E2EE primitives

Important security boundaries:

- the E2EE layer is not yet a full WhatsApp- or Signal-grade ratcheting deployment model (multi-device key lifecycle is still incomplete)
- there has been no formal external security audit
- distributed trust, abuse prevention, and high-risk production controls need more work

Mana can be used securely for many normal MVP scenarios, but it should not be marketed yet as audited, zero-compromise, or WhatsApp-class secure.

## ⚠️ Known Limitations

- No official CLI like `mana init` or `mana run`
- WebSocket backend abstraction exists, but only one main production backend is wired today
- Durable messaging and offline sync are partial, not fully product-complete
- Multi-device support is partial and still evolving
- Broker-backed clustering and tracing still need production deployment templates and operational validation
- RTC hardening still needs longer soak coverage for TURN-heavy and network-switch-heavy environments
- Long soak testing and failure-mode coverage are still limited

## 📊 Benchmarks And Validation

The repo includes benchmark and load-test work for:

- internal message routing
- room broadcast
- signaling fanout
- real WebSocket transport
- RTC offer handling
- production-style WebSocket load profiles

Useful files:

- [benchmark.md](benchmark.md)

## 📂 Examples

- [`cmd/example`](cmd/example)
- [`examples/full`](examples/full)
- [`examples/custom_db`](examples/custom_db) (Database Integration & Shared Transactions)
- [`examples/notification`](examples/notification)
- [`examples/sfu`](examples/sfu)
- [`kuruvi/`](kuruvi/) (A WhatsApp-like reference implementation)

## 📚 Resources & Documentation

- [**Live Documentation & Examples**](https://aswanidev-vs.github.io/Mana/)
- [**api/api.md**](api/api.md)
- [**prd.md**](prd.md)
- [**timeline.md**](timeline.md)
- [**benchmark.md**](benchmark.md)

## 🎯 Release Guidance

The honest way to release Mana on GitHub right now is as:

- `v0.3.0`
- early-stage
- experimental or beta
- suitable for MVPs and controlled deployments

That framing matches the current codebase better than calling it fully production-hardened.

## ⚖️ License

Mana is licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**. 

Please see the [LICENSE](LICENSE) file for the full text. This license ensures that improvements made to the framework remain available to the community, even when used over a network.
