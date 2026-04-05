# Mana Framework Hardening - Changelog 🌀

This document tracks all reliability and production-readiness improvements made during the framework audit.

## Phase 1: Observability & Consistency
*Focus: Standardizing internal boundaries and enabling production logging.*

- **Standardized Session ID Separators**:
  - **Change**: Updated the framework to use `::` as the consistent separator for composite keys (e.g., `userID::deviceID`).
  - **Problem Fixed**: Ambiguity during device lookups when user IDs contained hyphens or underscores. Ensures reliable multi-device routing.
- **Structured Transport Logging**:
  - **Change**: Replaced raw `fmt.Printf` calls in the WebSocket transport layer with the framework's native `observ.Logger`.
  - **Optimization**: Enables production-grade log filtering and structured diagnostics for connection lifecycle events.
- **Graceful Database Shutdown**:
  - **Change**: Explicitly closing the SQL backend connection pool in `App.Shutdown()`.
  - **Problem Fixed**: Resource leaks and "hanging" processes during server restart in high-concurrency environments.
- **Initialization Resilience**:
  - **Change**: Added proper error checking and logging during SQL store setup in `app.go`.
  - **Optimization**: Prevents silent failures during startup, providing immediate feedback on misconfigured database credentials.

---

## Phase 2: Atomic State & Resource Lifecycle
*Focus: Concurrency safety and global cleanup.*

- **Atomic PreKey Consumption**:
  - **Change**: Wrapped E2EE one-time prekey consumption in a database transaction (`RunInTx`).
  - **Problem Fixed**: Race conditions where multiple clients could be handed the same prekey simultaneously, leading to X3DH negotiation failures.
- **Global WebRTC Cleanup**:
  - **Change**: Implemented `rtc.Manager.Close()` to iterate and terminate all active peer connections.
  - **Problem Fixed**: "Zombie" network ports and memory leaks caused by orphaned WebRTC connections staying open after a server stop.
- **Signaling Timeout Standardization**:
  - **Change**: Unified disparate signaling timeouts using `defaultSignalTimeout` and `longSignalTimeout` constants.
  - **Optimization**: Prevents handlers from hanging indefinitely during unstable network conditions while allowing enough time for heavy SDP negotiations.
- **Frontend Security Headers (CSP)**:
  - **Change**: Added a Content Security Policy (CSP) meta tag to the Kuruvi reference implementation.
  - **Problem Fixed**: Mitigation against XSS (Cross-Site Scripting) and unauthorized data exfiltration in the demo browser client.

---
<!-- 
### Phase 3: Reliability & Persistence (Completed)
*Hardening for production stability and data integrity.*

- **Database Pool Unification**:
  - **Status**: Completed.
  - **Change**: Replaced 15+ manual `sql.Open` calls in the Kuruvi backend with the shared `app.DBBackend()` pool.
  - **Result**: Zero "database is locked" errors and significantly reduced overhead.
- **Versioned E2EE Storage**:
  - **Status**: Completed.
  - **Change**: Implemented `v1:Base64` layer in `keystore.go` with zero-copy `bytes.HasPrefix` optimizations.
  - **Result**: Fixed E2EE data corruption bugs caused by SQL driver string handling.
- **Secure Frontend Persistence**:
  - **Status**: Completed.
  - **Change**: Implemented **Encryption-at-Rest** for `localStorage` chat history.
  - **Result**: Chat history persists across restarts without compromising security (only encrypted payloads are stored on disk).
- **WebSocket Reconnector**:
  - **Status**: Completed.
  - **Change**: Added exponential backoff reconnector with visual HUD in the sidebar.
  - **Result**: UI remains responsive and automatically reconnects after server downtime.
-->