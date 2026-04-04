Mana is a Go framework for building real-time communication apps with WebSocket messaging, signaling, rooms, notifications, and WebRTC building blocks.

**Live Documentation & Examples**: [aswanidev-vs.github.io/Mana/](https://aswanidev-vs.github.io/Mana/)

It is designed for projects like:

- chat apps
- team communication tools
- live collaboration products
- WhatsApp- or Telegram-like MVPs
- signaling backends for audio/video features

Mana is in an early framework stage: strong enough for prototypes, demos, internal tools, and controlled MVP deployments, but not yet a WhatsApp-scale or fully hardened distributed production platform.

## Documentation

Start here depending on what you need:

- [**Live Documentation Website**](https://aswanidev-vs.github.io/Mana/): High-fidelity interactive guides and examples
- [api/api.md](api/api.md): developer guide and implementation manual
- [prd.md](prd.md): product scope and original vision
- [timeline.md](timeline.md): progress and roadmap
- [benchmark.md](benchmark.md): benchmark and load-test notes

## Features

- WebSocket messaging with acknowledgments
- Room and session management
- 1:1 and group communication primitives
- Presence and typing events
- WebRTC signaling and RTC manager integration
- Notification hub for direct user alerts
- JWT authentication and RBAC
- Rate limiting and origin controls
- TLS support and configurable server timeouts
- Metrics, health endpoints, and graceful shutdown
- OpenTelemetry tracing with in-memory inspection endpoint
- E2EE primitives and X3DH-style key exchange helpers
- Multi-session and device-aware connection tracking
- Durable message store and cursor-based reconnect sync
- Plug-and-play SQL "Batteries" (PostgreSQL, MySQL, SQLite) for Messaging, Identity, Social, and Settings
- Shared DB Transactions enabled via context-aware operations
- Stateless multi-node signal fanout via memory, Redis, or NATS backends
- TURN-aware WebRTC config with ICE relay policy and network recovery hooks

## Project Layout

- [app.go](app.go): main framework entry point
- [core](core): shared types and config
- [ws](ws): WebSocket abstractions and handler
- [signaling](signaling): signaling hub and router
- [room](room): room and session management
- [rtc](rtc): WebRTC and SFU-oriented pieces
- [e2ee](e2ee): encryption and key exchange helpers
- [notification](notification): user-targeted notifications
- [observ](observ): logging, metrics, and health helpers
- [storage](storage): message persistence and sync store
- [examples](examples): example applications
- [tmp telegram-clone](tmp%20telegram-clone): framework-backed demo app

## Install

```bash
go get github.com/Aswanidev-vs/mana
```

## Quick Start

```go
package main

import (
	"log"
	"time"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
)

func main() {
	app := mana.New(core.Config{
		Host:               "localhost",
		Port:               8080,
		EnableAuth:         false,
		EnableRTC:          true,
		EnableE2EE:         true,
		AllowedOrigins:     []string{"*"},
		MaxMessageSize:     1 << 20,
		ReadTimeout:        15 * time.Second,
		WriteTimeout:       15 * time.Second,
		IdleTimeout:        60 * time.Second,
		GracefulShutdownTimeout: 10 * time.Second,
	})

	app.OnMessage(func(msg core.Message) {
		log.Printf("message in room %s from %s", msg.RoomID, msg.SenderID)
	})

	log.Fatal(app.Start())
}
```

For a much more detailed implementation guide, including code examples for chat rooms, DMs, auth, notifications, RTC signaling, multi-device sessions, and production-minded setup, see [api/api.md](api/api.md).

## What You Can Build

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

## Current Production Position

Mana is suitable today for:

- prototypes
- college projects
- hackathons
- startup MVPs
- internal or controlled production workloads

Use caution before calling it production-ready for broad public internet scale. The framework still needs more work in distributed scaling, tracing, advanced E2EE session lifecycle, and large-scale operational hardening.

## Security Notes

Mana already includes a meaningful baseline:

- JWT auth support
- RBAC hooks
- origin controls
- rate limiting
- maximum message size enforcement
- TLS support
- graceful connection cleanup
- E2EE primitives

Important security boundaries:

- the E2EE layer is not yet a full WhatsApp- or Signal-grade ratcheting system
- there has been no formal external security audit
- multi-device key lifecycle is still incomplete
- distributed trust, abuse prevention, and high-risk production controls need more work

Mana can be used securely for many normal MVP scenarios, but it should not be marketed yet as audited, zero-compromise, or WhatsApp-class secure.

## Known Limitations

- No official CLI like `mana init` or `mana run`
- WebSocket backend abstraction exists, but only one main production backend is wired today
- Durable messaging and offline sync are partial, not fully product-complete
- Multi-device support is partial and still evolving
- Broker-backed clustering and tracing still need production deployment templates and operational validation
- RTC hardening still needs longer soak coverage for TURN-heavy and network-switch-heavy environments
- E2EE is primitive/foundation-level, not a complete ratcheting deployment model
- Long soak testing and failure-mode coverage are still limited

## Benchmarks And Validation

The repo includes benchmark and load-test work for:

- internal message routing
- room broadcast
- signaling fanout
- real WebSocket transport
- RTC offer handling
- production-style WebSocket load profiles

Useful files:

- [benchmark.md](benchmark.md)
<!-- - [websocket_e2e_benchmark_test.go](websocket_e2e_benchmark_test.go)
- [load_profile_test.go](load_profile_test.go) -->
<!-- 
Run tests:

```bash
go test ./...
```

Run benchmarks:

```bash
go test -run ^$ -bench . -benchmem ./...
``` -->

## Examples

- [cmd/example](cmd/example)
- [examples/full](examples/full)
- [examples/custom_db](examples/custom_db) (Database Integration & Shared Transactions)
- [examples/notification](examples/notification)
- [examples/sfu](examples/sfu)
- tmp telegram-clone

## Docs

- [**Live Documentation & Examples**](https://aswanidev-vs.github.io/Mana/)
- [api/api.md](api/api.md)
- [prd.md](prd.md)
- [timeline.md](timeline.md)
- [benchmark.md](benchmark.md)

## Release Guidance

The honest way to release Mana on GitHub right now is as:

- `v0.x`
- early-stage
- experimental or beta
- suitable for MVPs and controlled deployments

That framing matches the current codebase better than calling it fully production-hardened.

## License

Add your project license here before publishing.
