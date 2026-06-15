# Cross-Repository Reference

> **Purpose**: Quick lookup for working across both Tower repositories.  
> **Canonical location**: `tower-server/docs/CROSS_REPO_REFERENCE.md` (mirrored to Tower)

---

## Repository Roles

| Repo | Language | Purpose |
|------|----------|---------|
| `tower-server` | Go | Protocol bridge: vehicles ↔ UI. Owns wire format. |
| `Tower` | TypeScript/React (PWA) | Operator UI. Pure WebSocket client. |

---

## File Mapping

| Server (Go) | UI (TypeScript) | Notes |
|--------------|-----------------|-------|
| `api/proto/pidgin.proto` | — | Protobuf schema (vehicle↔server only) |
| `internal/protocol/frame.go` | `src/types/index.ts` | **SOURCE OF TRUTH** for JSON types |
| `internal/protocol/translate.go` | — | Proto→JSON conversion |
| `internal/protocol/builders.go` | — | Server-originated frames (welcome, error) |
| `internal/websocket/server.go` | `src/renderer/comms/connection.ts` | WebSocket client |
| `internal/websocket/client.go` | — | Client connection handling |
| `internal/registry/registry.go` | `src/renderer/stores/vehicleStore.ts` | Fleet state management |
| `internal/extensions/*.go` | `src/types/index.ts` (ExtensionManifest) | Extension codec + manifest |
| `testdata/protocol/*.json` | `testdata/protocol/*.json` | **MUST BE IDENTICAL** |

---

## Data Flow

```
┌──────────┐    UDP multicast     ┌──────────┐    WebSocket     ┌──────────┐
│ Vehicle  │──────────────────────│ Server  │──────────────────│    UI    │
│ (proto)  │  239.255.0.1:14550   │  (Go)    │ localhost:9000   │ (TS/React)│
└──────────┘                      └──────────┘                  └──────────┘
```

### Inbound (Vehicle → UI)

```
1. Vehicle sends VehicleTelemetry (protobuf)
   └─▶ internal/telemetry/multicast.go:Start()

2. Server decodes protobuf
   └─▶ internal/protocol/translate.go:DecodeVehicleMessage()

3. Server validates & translates to JSON Frame
   └─▶ internal/protocol/translate.go:TelemetryToFrame()

4. Server broadcasts to all clients
   └─▶ internal/websocket/server.go:Broadcast()

5. UI receives JSON, batches by animation frame
   └─▶ src/renderer/comms/connection.ts

6. UI renderer updates store
   └─▶ src/renderer/stores/vehicleStore.ts:applyTelemetry()
```

### Outbound (UI → Vehicle)

```
1. Operator clicks command button
   └─▶ src/renderer/components/ (Fleet panel flyout)

2. UI sends JSON command frame
   └─▶ src/renderer/comms/connection.ts

3. Server validates, rate-limits, converts to protobuf
   └─▶ internal/command/router.go:Route()

4. Server broadcasts on vehicle multicast
   └─▶ UDP 239.255.0.2:14551
```

---

## Type Correspondence

### Frame Envelope

| Go (`frame.go`) | TypeScript (`types/index.ts`) | JSON Key |
|-----------------|-------------------------------|----------|
| `Frame.ProtocolVersion` | `ProtocolFrame.protocolVersion` | `protocolVersion` |
| `Frame.Type` | `ProtocolFrame.type` | `type` |
| `Frame.VehicleID` | `ProtocolFrame.vehicleId` | `vehicleId` |
| `Frame.TimestampMs` | `ProtocolFrame.timestampMs` | `timestampMs` |
| `Frame.ServerTimestampMs` | `ProtocolFrame.serverTimestampMs` | `serverTimestampMs` |
| `Frame.Data` | `ProtocolFrame.data` | `data` |

### Telemetry Payload

| Go | TypeScript | JSON Key | Notes |
|----|------------|----------|-------|
| `TelemetryPayload.Location.Lat` | `location.lat` | `data.location.lat` | |
| `TelemetryPayload.Location.Lng` | `location.lng` | `data.location.lng` | |
| `TelemetryPayload.Location.AltMsl` | `location.altMsl` | `data.location.altMsl` | Optional; ground vehicles may omit |
| `TelemetryPayload.Speed` | `speed` | `data.speed` | m/s |
| `TelemetryPayload.Heading` | `heading` | `data.heading` | degrees [0, 360) |
| `TelemetryPayload.Environment` | `environment` | `data.environment` | `"air"` \| `"ground"` \| `"marine"` \| `"unknown"` |
| `TelemetryPayload.Seq` | `seq` | `data.seq` | Monotonic uint32, wraps; for ordering only |
| `TelemetryPayload.BatteryPercent` | `batteryPercent` | `data.batteryPercent` | Optional/null if unknown |
| `TelemetryPayload.SignalStrength` | `signalStrength` | `data.signalStrength` | 0-5 bars, optional/null if unknown |
| `TelemetryPayload.SupportedExtensions` | `supportedExtensions` | `data.supportedExtensions` | Namespaces this vehicle has active |
| `TelemetryPayload.Extensions` | `extensions` | `data.extensions` | Decoded extension telemetry by namespace |

### Status Values

| Concept | Go const | TypeScript | JSON value |
|---------|----------|------------|------------|
| Online | `StatusOnline` | `'online'` | `"online"` |
| Offline | `StatusOffline` | `'offline'` | `"offline"` |
| Standby | `StatusStandby` | `'standby'` | `"standby"` |

### Environment Values

| Concept | Protobuf | JSON |
|---------|----------|------|
| Aerial | `ENV_AIR` | `"air"` |
| Ground | `ENV_GROUND` | `"ground"` |
| Marine | `ENV_MARINE` | `"marine"` |

---

## Message Types

| Type | Direction | Go const | Droppable |
|------|-----------|----------|-----------|
| `telemetry` | Vehicle→UI | `TypeTelemetry` | Yes |
| `heartbeat` | Vehicle→UI | `TypeHeartbeat` | Yes |
| `status` | Server→UI | `TypeStatus` | No |
| `command_ack` | Vehicle→UI | `TypeCommandAck` | No |
| `fleet_status` | Server→UI | `TypeFleetStatus` | No |
| `alert` | Server→UI | `TypeAlert` | No |
| `welcome` | Server→UI | `TypeWelcome` | No |
| `error` | Server→UI | `TypeError` | No |
| `hello` | UI→Server | `TypeHello` | No |
| `command` | UI→Vehicle | `TypeCommand` | No |

---

## Extension System

| Concept | Server Location | UI Location |
|---------|------------------|-------------|
| Codec registration | `internal/extensions/registry.go` | — |
| Manifest YAML | `internal/extensions/{name}/manifest.yaml` | — |
| Manifest types | `internal/extensions/manifest.go` | `ExtensionManifest` in types |
| Decoded telemetry | `extensions` map in Frame | `VehicleInstance.extensions` |
| Capability advertisement | `VehicleCapabilities.Extensions` | `VehicleCapabilities.extensions` |

---

## Configuration

| Setting | Server Env Var | Default |
|---------|-----------------|---------|
| WebSocket port | `TOWER_WS_PORT` | `9000` |
| Telemetry multicast | `TOWER_MCAST_SOURCES` | `239.255.0.1:14550` |
| Command multicast | `TOWER_CMD_MCAST_GROUP` | `239.255.0.2:14551` |
| Standby timeout | `TOWER_STANDBY_TIMEOUT` | `3s` |
| Offline timeout | `TOWER_OFFLINE_TIMEOUT` | `10s` |
| Command rate limit | `TOWER_CMD_RATE_LIMIT` | `10/sec/vehicle` |

---

## Test Data Contract

Files in `testdata/protocol/` **MUST be identical** across both repos:

```
testdata/protocol/
├── commands.json      # UI→Server command examples
├── heartbeat.json     # Vehicle heartbeat with capabilities
├── responses.json     # Command ack examples
├── telemetry.json     # Vehicle telemetry frame
└── welcome.json       # Server handshake response
```

**Validation**: Run `diff -r` between repos before release.

---

## Implementation Status

| Component | Status | Location |
|-----------|--------|----------|
| Server WebSocket server | ✅ Done | `internal/websocket/` |
| Server telemetry listener | ✅ Done | `internal/telemetry/` |
| Server command routing | ✅ Done | `internal/command/` |
| UI WebSocket client | ✅ Done | `src/renderer/comms/connection.ts` |
| UI telemetry store updates | ✅ Done | `src/renderer/stores/vehicleStore.ts` |
| UI WebSocket client (Web Worker scale) | ⏳ Planned | See Tower `docs/COMMS_PIPELINE.md` |
