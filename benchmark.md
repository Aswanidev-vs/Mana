# Benchmark Results

This document records benchmark coverage and results for the Mana framework as measured on `2026-04-02`.

## Scope

The benchmark suite now covers three layers of the framework:

- Internal control-plane hot paths
  - room membership and room broadcast
  - signaling hub send, room broadcast, and signal parsing
  - app-level WebSocket message processing
- Real WebSocket transport
  - actual `ws://` clients connected to the framework handler
  - room message broadcast over live WebSocket connections
  - WebRTC-style signaling message forwarding over live WebSocket connections
- RTC / SFU media-side paths
  - PeerConnection creation
  - SDP offer handling through the framework RTC manager
  - RTP fan-out to subscribers
  - jitter-buffer packet ingestion

What this does not yet measure:

- browser-rendered client behavior
- TURN relay cost
- real camera/microphone encode/decode cost
- long-running multi-minute soak tests
- end-to-end live RTP from browsers into the SFU

## GPU Note

These benchmarks are CPU-side benchmarks.

The Go benchmark output shows:

- `cpu: AMD Ryzen 5 5600H with Radeon Graphics`

That string is the CPU model name reported by the system. It does not mean the benchmark used the Radeon GPU for compute.

The current Mana benchmarked paths do not use:

- CUDA
- DirectX / Vulkan compute
- NVENC / NVDEC
- GPU-backed media transforms

So the discrete `NVIDIA GTX 1650` was not part of the measured execution path. To produce a meaningful GPU benchmark, the framework would first need GPU-aware media or compute code.

## Environment

- Date: `2026-04-02`
- OS: `Windows`
- Arch: `amd64`
- CPU: `AMD Ryzen 5 5600H with Radeon Graphics`
- Go benchmark mode: `go test -run ^$ -bench . -benchmem ./...`
- Go cache override used for reproducibility in this workspace:
  - `GOCACHE=g:\Mana\.gocache`


## Results

### App / WebSocket Control Path

| Benchmark | Result |
| --- | --- |
| `BenchmarkAppOnWSMessage_Message` | `12285 ns/op`, `1690 B/op`, `30 allocs/op` |
| `BenchmarkAppOnWSMessage_MessageWithAck` | `14517 ns/op`, `1826 B/op`, `35 allocs/op` |

Interpretation:

- The in-process app message path is in the low-microsecond range.
- ACK support adds a measurable but modest extra cost.
- Most of this path is JSON and routing overhead rather than transport cost.

### Real WebSocket Transport

| Benchmark | Result |
| --- | --- |
| `BenchmarkWebSocketRoomBroadcast_10Clients` | `916316 ns/op`, `21013 B/op`, `297 allocs/op` |
| `BenchmarkWebSocketRTCSignalingOfferForward_2Clients` | `259093 ns/op`, `5603 B/op`, `85 allocs/op` |

Interpretation:

- Real socket transport is substantially more expensive than in-process routing, as expected.
- Room broadcast over live sockets is just under `1 ms/op` for `10` connected clients in this setup.
- WebRTC-style signaling forwarding over live sockets is around `0.26 ms/op`.

### Production-Style WebSocket Load Profile

The following load profile used real `ws://` clients against the framework handler and measured:

- total connection setup time
- average connection setup time per client
- average fan-out round-trip time for one sender broadcasting to the room
- slowest round observed

Results from `TestWebSocketProdProfile` on this machine:

| Clients | Connect Total | Avg Connect / Client | Avg Broadcast Round | Slowest Round | Delivered Messages / Round |
| --- | --- | --- | --- | --- | --- |
| `25` | `49.2293 ms` | `1.969172 ms` | `1.11256 ms` | `2.0277 ms` | `25` |
| `50` | `141.3528 ms` | `2.827056 ms` | `1.70556 ms` | `2.2376 ms` | `50` |
| `100` | `545.9433 ms` | `5.459433 ms` | `3.16944 ms` | `3.5452 ms` | `100` |
| `250` | `2.9761248 s` | `11.904499 ms` | `9.452633 ms` | `10.0316 ms` | `250` |

Interpretation:

- On this laptop, the framework handled `250` concurrent WebSocket clients in this synthetic room-broadcast test without failure.
- Connection setup cost increases noticeably as concurrent client count rises.
- Broadcast fan-out remained under `10.1 ms` at `250` clients in this local test.
- This should be treated as a local capacity signal, not a production guarantee.

### Room Manager

| Benchmark | Result |
| --- | --- |
| `BenchmarkRoomBroadcast_10Members` | `1093 ns/op`, `1151 B/op`, `0 allocs/op` |
| `BenchmarkRoomBroadcast_100Members` | `8753 ns/op`, `13636 B/op`, `0 allocs/op` |
| `BenchmarkManagerJoinLeave` | `1511 ns/op`, `574 B/op`, `8 allocs/op` |

Interpretation:

- Room fan-out scales roughly with member count.
- The room-level path itself is cheap; network and higher-level signaling dominate once real sockets are involved.

### Signaling Hub

| Benchmark | Result |
| --- | --- |
| `BenchmarkHubSend` | `770.2 ns/op`, `487 B/op`, `3 allocs/op` |
| `BenchmarkHubBroadcastToRoom_10Peers` | `1514 ns/op`, `1508 B/op`, `3 allocs/op` |
| `BenchmarkHubBroadcastToRoom_100Peers` | `7900 ns/op`, `13771 B/op`, `3 allocs/op` |
| `BenchmarkHubHandleMessage` | `2510 ns/op`, `925 B/op`, `13 allocs/op` |

Interpretation:

- The signaling hub is fast in isolation.
- Message parsing and marshaling account for much of the cost.
- Broadcast cost scales linearly with room size, which is expected for the current design.

### RTC Manager

| Benchmark | Result |
| --- | --- |
| `BenchmarkManagerCreatePeerConnection` | `452939 ns/op`, `64586 B/op`, `807 allocs/op` |
| `BenchmarkManagerHandleOffer` | `67411816 ns/op`, `315221 B/op`, `1790 allocs/op` |

Interpretation:

- PeerConnection creation is around `0.45 ms/op`.
- Full framework-side SDP offer handling is much heavier at about `67 ms/op`.
- This is the most expensive benchmarked path and is the clearest RTC setup bottleneck in the current codebase.

### SFU / Media Plane

| Benchmark | Result |
| --- | --- |
| `BenchmarkRouterFanOut_10Subscribers` | `2212 ns/op`, `1280 B/op`, `1 allocs/op` |
| `BenchmarkRouterFanOut_100Subscribers` | `10692 ns/op`, `1280 B/op`, `1 allocs/op` |
| `BenchmarkJitterBufferPush_InOrder` | `14060 ns/op`, `144 B/op`, `1 allocs/op` |
| `BenchmarkJitterBufferPush_WithLossPattern` | `7375 ns/op`, `302 B/op`, `4 allocs/op` |

Interpretation:

- RTP fan-out in the router is relatively cheap and scales with subscriber count.
- The current fan-out path still pays per-packet clone cost.
- Jitter-buffer handling is measurable and should be considered part of the media-plane budget.

## Files Added For Benchmarking

- [app_benchmark_test.go](/g:/Mana/app_benchmark_test.go)
- [load_profile_test.go](/g:/Mana/load_profile_test.go)
- [websocket_e2e_benchmark_test.go](/g:/Mana/websocket_e2e_benchmark_test.go)
- [manager_benchmark_test.go](/g:/Mana/room/manager_benchmark_test.go)
- [hub_benchmark_test.go](/g:/Mana/signaling/hub_benchmark_test.go)
- [manager_benchmark_test.go](/g:/Mana/rtc/manager_benchmark_test.go)
- [router_benchmark_test.go](/g:/Mana/rtc/router_benchmark_test.go)

## Framework Issues Found During Benchmarking

The benchmark work exposed two real framework bugs that were fixed:

- Auth-disabled RBAC path could panic because a typed-nil RBAC adapter was still callable.
  - Fixed in [app.go#L388](/g:/Mana/app.go#L388)
- RTC offer handling could enter an invalid signaling state by trying to create an answer twice.
  - Fixed in [manager.go#L478](/g:/Mana/rtc/manager.go#L478)

## High-Level Takeaways

- Internal room and signaling primitives are fast.
- Real WebSocket transport is an order of magnitude more expensive than in-process routing, which is normal.
- The heaviest setup cost is WebRTC offer handling.
- The SFU fan-out path is reasonably lightweight for synthetic RTP packets, but true browser-driven media load should still be tested separately.

## Recommended Next Benchmarks

- multi-room concurrent WebSocket load with hundreds of clients
- long-lived RTC soak benchmark with repeated join/leave cycles
- publish/subscribe benchmark using real local tracks across connected PeerConnections
- CPU and memory profiling during `BenchmarkManagerHandleOffer`
