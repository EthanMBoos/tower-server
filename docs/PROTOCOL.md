# Tower Server Protocol Specification

> Behavioral contracts for server ↔ UI communication. The proto defines message shapes; this doc defines what the server does with them and what clients must do in response.  
> Types: [`internal/protocol/`](../internal/protocol/) — Terminology: [GLOSSARY.md](GLOSSARY.md)

---

## Transport

```
Radio Node ◀───protobuf/UDP multicast───▶ Server ◀───JSON/WebSocket───▶ UI Client
```

| Direction | Format | Transport | Address |
|-----------|--------|-----------|---------|
| Vehicle → Server | protobuf | UDP multicast | `239.255.0.1:14550` |
| Server → Vehicle | protobuf | UDP multicast | `239.255.0.2:14551` |
| Server ↔ UI | JSON | WebSocket | `ws://localhost:9000` |

**Security (MVP):** Trusted LAN only. No authentication, authorization, or encryption.

---

## Connection

```
UI ──── hello ────────────────────────▶ Server
UI ◀─── welcome (fleet, manifests) ─── Server

Vehicle ──── VehicleTelemetry ────────▶ Server ──── telemetry ────▶ UI
Vehicle ──── Heartbeat (capabilities) ▶ Server ──── heartbeat ───▶ UI

UI ──── command ──────────────────────▶ Server ──── multicast ───▶ Vehicle
Vehicle ─── CommandAck ───────────────▶ Server ──── command_ack ▶ UI
```

The client MUST send `hello` first. The server responds with `welcome` — a full fleet snapshot plus available extensions and manifests. Only clients that have completed the handshake receive subsequent broadcasts.

### Reconnect: Replace, Don't Merge

On disconnect (network drop, server restart, refresh), the client MUST replace its local fleet state with the new `welcome` snapshot — not merge it.

| Scenario | Merge (wrong) | Replace (correct) |
|----------|---------------|-------------------|
| Vehicle removed while disconnected | Ghost vehicle persists | Vehicle disappears |
| Vehicle status changed | Stale status shown | Correct status from snapshot |
| Server restarted | Client shows vehicles server doesn't know | Clean slate |

---

## Message Types

| Type | Direction | Droppable | Notes |
|------|-----------|-----------|-------|
| `telemetry` | Server → UI | Yes | 10-100Hz, position/heading/battery |
| `heartbeat` | Server → UI | Yes | 1Hz, vehicle capabilities |
| `status` | Server → UI | No | Online/standby/offline transitions |
| `command_ack` | Server → UI | No | Command response (server or vehicle) |
| `alert` | Server → UI | No | Warnings, geofence, system events |
| `fleet_status` | Server → UI | No | Fleet summary |
| `welcome` | Server → UI | No | Handshake response |
| `error` | Server → UI | No | Protocol or system error |
| `hello` | UI → Server | No | Required first message |
| `command` | UI → Server | No | Vehicle command |

`vehicleId: "_server"` marks server-originated frames (status, welcome, alerts).

---

## Telemetry Pipeline

### Sequence Numbers

Telemetry arrives over UDP and may be out-of-order or duplicated. Use `seq` for ordering — never timestamps. Vehicle clocks are **untrusted** (no RTC/NTP). Server timestamps (`serverTimestampMs`) are authoritative.

The server maintains a **high-water mark (HWM)** per vehicle:

| Condition | Action |
|-----------|--------|
| `seq > HWM` | Accept, update HWM |
| `seq ≤ HWM` | Drop — duplicate or stale retransmit |
| Wrap near 2^32 → 0 | Accept if `(seq − HWM)` as int32 is positive |

Implementation: [`internal/protocol/sequence.go`](../internal/protocol/sequence.go)

### Vehicle Reboot

When a vehicle reboots, `seq` resets to 0. Without intervention, all new telemetry would be dropped (HWM still at the pre-reboot value).

**Contract:** The registry MUST call `SequenceTracker.Reset(vehicleID)` on every `offline → online` transition. Tying reset to this transition is unambiguous — a seq regression alone is not a reliable reboot signal (could be wrap-around or multi-node counters).

Implementation: [`internal/registry/registry.go`](../internal/registry/registry.go)

### Staleness (UI Responsibility)

The server does not filter by timestamp. The UI detects stale data via seq gaps:

| Condition | Action |
|-----------|--------|
| Normal increment | Display |
| Gap > 1 | Packet loss — display, optionally warn |
| Gap > 100 | Major loss or recovery — consider "recovering" state |
| No telemetry > 3s | Server emits a `status` frame (standby/offline) |

---

## Vehicle Status

```
                  telemetry
                ┌───────────┐
                ▼           │
┌─────────┐  telemetry  ┌─────────┐
│ OFFLINE │────────────▶│ ONLINE  │◀─── first telemetry from new vehicle
└─────────┘             └─────────┘
     ▲                       │
     │ 10s no telemetry      │ 3s no telemetry
     │                       ▼
     │                  ┌─────────┐
     └──────────────────│ STANDBY │
                        └─────────┘
```

Timeouts are configurable: `TOWER_STANDBY_TIMEOUT` (default 3s), `TOWER_OFFLINE_TIMEOUT` (default 10s).

Each transition emits a guaranteed `status` frame — the UI never needs its own timeout timer for vehicle state.

Vehicles should send heartbeat every 1s ± 100ms. Telemetry packets reset the heartbeat timer; no need to send both simultaneously.

**Vehicle-side server loss detection:** NOT IMPLEMENTED for MVP. `ServerHeartbeat` is reserved in the proto for future implementation. Vehicle failsafe is the vehicle firmware's responsibility.

---

## Commands

### Rate Limiting

Max **10 commands/second per vehicle**, counted globally across all connected clients. If Client A sends 6 and Client B sends 5 to the same vehicle in one second, the 11th is rejected — regardless of which client sent it. Rejected commands get an `error` frame (`code: "RATE_LIMITED"`) and are never forwarded to the vehicle.

### Delivery Flow

```
UI ──── command ─────────────────────────▶ Server ──── multicast ──▶ Vehicle
UI ◀─── command_ack { accepted } ─────────┘ (immediate, from server)

... vehicle responds (or 5s passes) ...

UI ◀─── command_ack { completed / failed / timeout } (from server, relaying vehicle)
```

The server sends an immediate `accepted` ack when it successfully broadcasts the command. The vehicle's response (completed/failed) is relayed asynchronously. If no response arrives within `TOWER_CMD_TIMEOUT` (default 5s), the server sends a synthetic `timeout` ack — the UI never needs its own command timeout.

### ACK Status Reference

| Status | Source | Meaning |
|--------|--------|---------|
| `accepted` | Server | Command broadcast to network |
| `rejected` | Server | Invalid command or vehicle not found |
| `failed` | Server | Network error |
| `accepted` | Vehicle | Vehicle received, will execute |
| `completed` | Vehicle | Action finished successfully |
| `failed` | Vehicle | Execution failed |
| `timeout` | Server (synthetic) | No vehicle response within timeout |

Treat `timeout` as "outcome unknown" — distinct from `rejected` (definitely not sent) or `failed` (definitely not executed).

### Idempotency

Commands include `commandId` (UUID). Vehicles filter by `vehicle_id` from the multicast broadcast and must treat duplicate commandIds as no-ops.

---

## Other Conventions

**Timestamps:** All timestamps are Unix epoch milliseconds (int64). Never use vehicle `timestampMs` for timeout logic or cross-vehicle ordering — use `serverTimestampMs`.

**Vehicle IDs:** `{type}-{platform}-{identifier}` — e.g., `ugv-husky-07`, `uav-skydio-x2d-03`. Reserved: `_server`, `_fleet`, `_client`.

**ENV_UNKNOWN:** `VehicleEnvironment` value `0` is accepted with a warning log. Proto3 unset enums default to `0`; rejecting them would silently drop telemetry from misconfigured vehicles.

**Protocol versioning:** All frames include `protocolVersion` (currently `1`). New optional fields don't bump the version. Breaking changes do; server supports current + one prior. Clients ignore unknown fields.

---

## Error Codes

| Code | Cause |
|------|-------|
| `INVALID_MESSAGE` | Malformed JSON |
| `UNKNOWN_COMMAND` | Unknown action |
| `VEHICLE_NOT_FOUND` | Vehicle not in registry |
| `RATE_LIMITED` | Too many commands |
| `PROTOCOL_VERSION_UNSUPPORTED` | Version mismatch |
| `COMMAND_SEND_FAILED` | Multicast network error |

---

## Implementation Reference

| File | Purpose |
|------|---------|
| [`internal/protocol/frame.go`](../internal/protocol/frame.go) | JSON wire types |
| [`internal/protocol/translate.go`](../internal/protocol/translate.go) | Proto ↔ JSON translation |
| [`internal/protocol/validate.go`](../internal/protocol/validate.go) | Message validation |
| [`internal/protocol/builders.go`](../internal/protocol/builders.go) | Frame constructors |
| [`internal/protocol/sequence.go`](../internal/protocol/sequence.go) | Sequence tracking & deduplication |
| [`api/proto/pidgin.proto`](../api/proto/pidgin.proto) | Protobuf schema |
