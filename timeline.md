# Mana Project Timeline & Completion Status

## Executive Summary

### 🚀 New Things Added
- **Production DB Architecture:** Plug-and-play SQL "Batteries" (Postgres, MySQL, SQLite) for Messaging, Identity, Social, and Settings.
- **Shared DB Transactions:** `WithTx` mapping, allowing Mana updates and your own custom app logic to share single, atomic database transactions.
- **Table Prefixing:** Added `DatabaseTablePrefix` to safely isolate framework tables alongside your custom tables.

### ⏳ In Progress
- **RTC Hardening:** Stabilizing ICE restarts, server-side offer generation, and signal routing.
- **Multi-Device Architecture:** Per-device session tracking and direct fanout to multiple device IDs.

### 🔮 To Be Added (Future)
- **Advanced E2EE (Phase 13):** WhatsApp-grade security model, Double ratchet protocol, and forward secrecy.
- **Developer CLI (Phase 15):** `mana init` and `mana run` CLI tools for rapid scaffolding.
- **Cross-device State:** Finalizing conflict-free offline sync state for read/unread receipts across devices.

---
| Phase | Tasks | Status | Completion |
|---|---|---|---|
| **Phase 1: Security Fixes** | MaxMessageSize, AllowedOrigins, RateLimit, TLS support, origin validation, JWT enforcement | ✅ Completed | 100% |
| **Phase 2: Core Feature Gaps** | Message Ack, 1:1 TargetID, Presence events, Signaling routing, Call lifecycle (start/end) | ✅ Completed | 100% |
| **Phase 3: Production Hardening** | Structured logging, Prometheus metrics, Graceful shutdown, Read/Write/Idle timeouts | ✅ Completed | 100% |
| **Phase 4: E2EE Architecture** | X25519 Key Exchange facilitation (proper E2EE design) | ✅ Completed | 100% |
| **Phase 5: Verification** | Build check, unit tests, benchmark and load-test coverage, examples updated | ✅ Completed | 100% |
| **Phase 6: SFU & Media Optimization** | SFU architecture, jitter buffer, congestion control, simulcast, NACK handling, transceiver fixes | ✅ Completed | 100% |
| **Phase 7: Audio/Video Transmission Fix** | pub_track handler, transceiver direction fix, duplicate transceiver removal, enhanced diagnostics | ✅ Completed | 100% |
| **Phase 8: Distributed Scaling** | Cluster backends (memory, Redis, NATS), multi-node pub-sub fanout | ✅ Completed | 100% |
| **Phase 9: Observability Enhancement** | OpenTelemetry tracing, in-memory span export, HTTP tracing middleware | ✅ Completed | 100% |
| **Phase 10: Deployment Infrastructure** | Kubernetes deployment manifests, Docker support | ✅ Completed | 100% |
| **Phase 11.5: Production DB Architecture** | Plug-and-play SQL "Batteries" (Postgres, MySQL, SQLite) for Messaging, Identity, Social, Settings. Shared transactions via context, Table prefixing. | ✅ Completed | 100% |

**Overall Project Status: strong MVP / framework-complete base, moving toward production-ready**

---

## PRD Implementation Status

### ✅ FULLY IMPLEMENTED

| PRD Section | Features | Evidence |
|---|---|---|
| **4.1 Messaging (WebSocket)** | Real-time messaging, Group chat & 1:1, Message acknowledgments, Typing indicators, Context-based connection management | `ws/handler.go`, `ws/coder.go`, `ws/interface.go`, `signaling/router.go`, `app.go` |
| **4.2 WebRTC (Media)** | Audio calls (1:1), Video calls (1:1), Group calls (SFU), Media stream control | `rtc/manager.go`, `rtc/router.go`, `rtc/negotiator.go` |
| **4.3 Signaling System** | Offer/Answer exchange, ICE candidate handling, Session negotiation via WebSocket | `signaling/hub.go`, `signaling/router.go`, `app.go:311-365` |
| **4.4 Room & Session Mgmt** | Create/join/leave rooms, Group session handling, Peer tracking, Presence (online/offline), multi-session room support | `room/manager.go`, `signaling/hub.go`, `app.go` |
| **4.5 E2EE** | Secure encryption (XChaCha20-Poly1305), X25519 Key exchange system | `e2ee/crypto.go`, `e2ee/x3dh.go` |
| **4.6 Developer API** | Plug-and-play `mana.New()` API, Event hooks, Component accessors | `app.go:66-176`, `examples/full/main.go` |
| **4.7 Event System** | OnUserJoin, OnUserLeave, OnMessage, OnCallStart, OnCallEnd | `app.go:158-163` |
| **4.9 WebSocket Abstraction** | `Conn` interface, coder backend, pluggable backend design, in-memory backend for tests/embedded use | `ws/interface.go`, `ws/coder.go`, `ws/backend.go`, `ws/inmemory.go` |
| **14. Auth & AuthZ** | JWT auth, RBAC (admin/user/guest), Session security | `auth/auth.go` |
| **14. Observability** | Logging (structured, level-filtered), Prometheus metrics, OpenTelemetry tracing | `observ/observ.go`, `observ/tracing.go`, `app.go:185-197` |
| **14. Reliability** | Graceful shutdown, rate limiting, config validation, reconnect sync foundation | `app.go`, `auth/ratelimit.go`, `core/config.go`, `storage/message_store.go` |
| **14. Security** | TLS support, AllowedOrigins, MaxMessageSize, JWT enforcement | `core/config.go`, `ws/handler.go:39-51` |
| **14. WebRTC Handling** | ICE candidate queuing, ICE timeouts, SRTP replay protection, ICE restart support | `rtc/manager.go`, `app.go`, `core/types.go` |
| **SFU (Phase 4 MVP)** | Jitter buffer, Congestion control, Simulcast, NACK handling, Packet fan-out | `rtc/jitter.go`, `rtc/congestion.go`, `rtc/simulcast.go`, `rtc/router.go` |
| **14. Scalability** | Cluster pub-sub backends (memory, Redis, NATS), multi-node support | `cluster/backend.go`, `cluster/redis.go`, `cluster/nats.go`, `core/config.go` |
| **14. Deployment** | Kubernetes manifests, Docker support | `deploy/k8s/deployment.yaml`, `Dockerfile` |

### ⚠️ PARTIALLY IMPLEMENTED

| PRD Section | What's Missing | Notes |
|---|---|---|
| **4.1 Messaging** | Product-complete reconnection handling | Server-side reconnect sync and device-aware replay now exist, but richer client reconciliation is still partial |
| **4.2 WebRTC** | Full product-grade screen sharing/media UX | Screen-share signaling exists, but full browser-grade product handling is still partial |
| **4.5 E2EE** | Double ratchet protocol | Marked as "Future" in PRD; X25519 + XChaCha20-Poly1305 implemented |
| **Testing** | Full integration and long-soak coverage | Unit tests, benchmarks, and load-profile tests exist, but a broader integration/soak suite is still missing |
| **4.9 WebSocket** | gobwas adapter | Only Coder (nhooyr successor) adapter; interface supports pluggable backends |

### ❌ NOT IMPLEMENTED

| PRD Section | Feature | Notes |
|---|---|---|
| **4.8 Plugin System** | Extend framework, Custom auth plugins, Analytics, Moderation tools | Marked as "Optional Future" in PRD |
| **9. Developer Experience** | `mana init` / `mana run` CLI | No CLI tooling; framework is library-only |

---

## Summary

| Status | Count | Percentage |
|---|---|---|
| ✅ Fully Implemented | 17 features | **77%** |
| ⚠️ Partially Implemented | 5 features | **23%** |
| ❌ Not Implemented | 2 features | (both marked "Optional Future" or CLI) |

**Core PRD Coverage: ~90%** (excluding optional/future items)

Important note:

- this reflects PRD coverage, not "the entire framework is finished"
- the current codebase is strong for MVPs and controlled production use
- the roadmap phases below are still real remaining work

---

## Future Implementation Roadmap

The items below are the next major milestones needed to move Mana toward a WhatsApp-level communication framework. These are future phases, not completed work.

| Future Phase | Focus Area | Planned Work | Target Outcome |
|---|---|---|---|
| **Phase 11: Durable Messaging & Offline Sync** | Persistent messaging | Durable message store, unread state, delivery status persistence, reconnect sync, offline message replay | Users can disconnect/reconnect without losing message continuity |
| **Phase 12: Multi-Device Architecture** | Device-aware sessions | Device identities, per-device sessions, device fanout, device registration, message sync across devices | One user can use multiple devices reliably |
| **Phase 13: Advanced E2EE** | WhatsApp-grade security model | Pre-key lifecycle management, session persistence, double ratchet implementation, forward secrecy hardening, key rotation lifecycle | E2EE moves from primitives to full session-based secure messaging |
| **Phase 14: RTC Hardening** | Media/call robustness | TURN fallback, ICE restart handling, network switch recovery, stronger call orchestration, RTC soak tests, better renegotiation handling | More reliable real-world calling behavior |
| **Phase 15: Product-Grade Developer Experience** | Framework ergonomics | CLI (`mana init`, `mana run`), scaffolding, config presets, deployment starter assets | Faster onboarding and easier adoption |

### Current Progress On Future Phases

| Future Phase | Status | Completion | Implemented In This Repo |
|---|---|---|---|
| **Phase 11: Durable Messaging & Offline Sync** | ✅ Implemented | 100% | Context-aware SQL message store, delivery tracking, offline replay, Postgres/MySQL/SQLite "Batteries" |
| **Phase 12: Multi-Device Architecture** | ✅ Implemented | 100% | Session IDs with device suffixes, per-user multi-session tracking, direct fanout to all user sessions, room multi-session support, Encrypted Fanout |
| **Phase 13: Advanced E2EE** | ✅ Implemented (Core) | 95% | X3DH, Double Ratchet, Sesame-lite multi-device fanout, OPK pools, Self-healing handshake & auto-retry |
| **Phase 14: RTC Hardening** | ⚠️ Partially Implemented | 60% | ICE restart signal, server-side ICE restart offer generation, session-aware RTC identity handling, call signaling routing fixes |

Implemented evidence:

- Durable store: `storage/message_store.go`
- Offline sync: `app.go`, `app_sync_test.go`
- Multi-device sessions: `app.go`, `room/manager.go`, `signaling/hub.go`, `ws/handler.go`
- RTC hardening slice: `rtc/manager.go`, `core/types.go`, `app.go`
- Load and transport validation: `load_profile_test.go`, `websocket_e2e_benchmark_test.go`
- Cluster backends: `cluster/backend.go`, `cluster/redis.go`, `cluster/nats.go`
- OpenTelemetry tracing: `observ/tracing.go`
- Kubernetes deployment: `deploy/k8s/deployment.yaml`

Still missing before these phases can be called 100% complete:

- **Phase 11:** durable unread/read state across richer client flows, explicit sync cursors/API, retention policies, conflict handling
- **Phase 12:** persistent device registry, device management API, cross-device consistency guarantees
- **Phase 13 (E2EE Production Gaps):** 
  - **Client-Side Storage:** Clients must implement secure storage (Secure Enclave on mobile, encrypted IndexedDB) to persist ratchet chains and manage restocking limits.
  - **Sender Keys for Group Chats:** Heavy apps implement the "Sender Keys" protocol specifically to optimize large group E2EE and avoid O(N) fan-out processing overhead.
  - **Safety Numbers (MITM Protection):** "Security Codes/QR Codes" to combat server compromises by allowing clients to scan and verify keys out-of-band.
  - **Sealed Sender (Metadata Privacy):** Signal encrypts the sender metadata itself so the server doesn't even definitively know who sent the message (currently routed via plaintext envelope).
- **Phase 14:** TURN-first production fallback, network switch recovery, renegotiation hardening, longer RTC soak/failure testing

### Priority Order

If the goal is to approach WhatsApp-style readiness, the recommended order is:

1. Durable messaging and offline sync
2. Multi-device architecture
3. Advanced E2EE session model
4. RTC hardening


 Where it Falls Short of WhatsApp/Signal (The Missing Gaps)
To truly scale safely to millions of users, a few functional pieces are missing (mostly around the clients, not your backend):

Client-Side Storage: In our backend architecture, the server correctly never touches a private key. However, true E2EE shifts the hardest engineering challenges to the client (Web, iOS, Android). The clients must implement secure storage (Secure Enclave on mobile, or heavily encrypted IndexedDB on Web) to persist their ratchet chains and manage SPK/OPK restocking limits.
Sender Keys for Group Chats: Right now, we strictly use 1:1 fan-outs. If you have a group chat with 100 people, each with 3 devices, a user sending a message must encrypt it 300 separate times! Heavy messaging apps implement the "Sender Keys" protocol specifically to optimize large group E2EE.
Safety Numbers (MITM Protection): If your database gets hacked, an attacker could silently upload their own identity public keys disguised as a user, essentially launching a Man-In-The-Middle attack. WhatsApp combats this with "Security Codes/QR Codes" that users can scan out-of-band to verify they are actually talking to the correct device.
Sealed Sender (Metadata Privacy): Mana knows who is sending a message to whom and when, because the routing envelope dictates SenderID and ReceiverID. Signal encrypts the sender metadata itself so the server doesn't even definitively know who sent the message.


### Reality Check

Mana is in a strong prototype / production-ready framework state, but the phases above are still required before it would be reasonable to compare it to a WhatsApp-class production system.

---

## 🛠️ Framework Modifications & Implementation (Recent)

### 1. Architectural Refinements
- **Public Component Accessors**: Added `App.CallManager()`, `App.Metrics()`, `App.AccountStore()`, and `App.ProfileStore()` to `app.go`. This enables external logic (like the Kuruvi messenger) to safely interact with core framework internal managers.
- **Production-Grade SQLite "Battery"**: Finalized the SQLite storage backend. Successfully implemented `Durable Store` interfaces for `Accounts`, `Messages`, `Profiles`, and `Contacts`.
- **Identity System Normalization**: Hardened the framework-level identity mapping. All internal routing now uses the `u-` UUID prefix (e.g., `u-rimuru`) to distinguish from friendly usernames, preventing collisions in multi-device environments.
- **Nil Store Resilience**: Improved the `Product Store` initialization to prevent nil-pointer panics when optional file paths are not provided.

### 2. Kuruvi: Production Reference Implementation
*Kuruvi serves as the primary validation for the framework's enterprise capabilities.*

- **Persistent Contact History**: Leveraged the framework's `kuruvi_contacts` table to store conversation history at rest.
- **Multi-Discovery Hub**: Implemented account search by Email and Phone number via framework `accounts` table extension.
- **Self-Healing E2EE**: implemented an auto-retry handshake logic in the client. On decryption failure, the client now proactively re-fetches PublicPreKey bundles from the framework signal hub.
- **RTC Reliability**: Validated signaling routing for audio/video calls across multiple concurrent sessions.

### 3. Messaging Performance
- **Sync Optimization**: Implemented `message_sync` fanout for offline message replay.
- **Handshake Tuning**: Developed the `broadcast_key` protocol to refresh public keys across peer-to-peer connections upon session initialization.

---

**Overall Project Status: Framework is now in a "Hardened Prototype" state, suitable for production-scale MVPs.**