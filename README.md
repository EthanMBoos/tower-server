# Tower-Server

Communications server for robotic fleets. Bridges commands and telemetry between vehicles and the [Tower](https://github.com/EthanMBoos/Tower) UI.

```
┌──────────────┐    UDP multicast    ┌──────────────┐    WebSocket     ┌──────────────┐
│  50+ Robots  │ ◀─────────────────▶ │   Server     │ ◀───────────────▶│  N Operator  │
│  10-100Hz    │   239.255.0.1:14550 │              │   localhost:9000 │     UIs      │
│  protobuf    │                     │              │   JSON frames    │              │
└──────────────┘                     └──────────────┘                  └──────────────┘
```

The server is a **standalone process** — no dependency on the UI. Multiple UI clients can connect simultaneously. Vehicles never communicate directly with the UI.

It provides four core capabilities:

1. **Protocol Translation** — Decodes protobuf from vehicles, encodes JSON for UIs
2. **Fleet Registry** — Tracks vehicle state (online/standby/offline) via telemetry gaps
3. **Command Routing** — Validates, rate-limits, and forwards commands to vehicles
4. **Extensibility** — Codec plugin system for custom vehicle protocols

## Quick Start

```bash
git clone https://github.com/EthanMBoos/tower-server.git
cd tower-server
go mod download

# Run server + simulated vehicles
./scripts/demo.sh

# Or manually:
go run ./cmd/tower-server &
go run ./cmd/testsender -vid ugv-husky-01

# Verify connection
go run ./cmd/testclient
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `TOWER_WS_PORT` | `9000` | WebSocket server port |
| `TOWER_MCAST_SOURCES` | `239.255.0.1:14550` | Telemetry multicast sources |
| `TOWER_CMD_MCAST_GROUP` | `239.255.0.2` | Command multicast group |
| `TOWER_CMD_MCAST_PORT` | `14551` | Command multicast port |
| `TOWER_STANDBY_TIMEOUT` | `3s` | Time before vehicle marked standby |
| `TOWER_OFFLINE_TIMEOUT` | `10s` | Time before vehicle marked offline |

**Multi-source example:**
```bash
TOWER_MCAST_SOURCES="239.255.0.1:14550:ugv,239.255.1.1:14551:usv" go run ./cmd/tower-server
```

## Project Structure

```
tower-server/
├── cmd/
│   ├── tower-server/   # Main entry point
│   ├── testsender/     # Vehicle simulator
│   └── testclient/     # WebSocket test client
├── api/proto/
│   └── pidgin.proto    # Core protocol schema
├── internal/
│   ├── protocol/       # Frame types, translation, validation, sequence tracking
│   ├── registry/       # Vehicle state machine (online/standby/offline)
│   ├── command/        # Command routing, rate limiting, ACK tracking
│   ├── telemetry/      # UDP multicast listener
│   ├── websocket/      # WebSocket server, client management
│   ├── extensions/     # Codec plugin registry
│   ├── config/         # Environment variable parsing
│   └── observability/  # Metrics, health endpoints
└── scripts/
    └── demo.sh         # Multi-vehicle demo launcher
```

## Architecture Highlights

**Sequence-based deduplication** — Vehicles send monotonic sequence numbers (`seq`). The server tracks a high-water mark (HWM) per vehicle — any `seq ≤ HWM` is dropped as a duplicate or stale retransmit. Handles UDP packet reordering without relying on untrusted vehicle clocks.

**Untrusted vehicle timestamps** — Vehicle clocks have no RTC or NTP. The server adds its own authoritative timestamp (`serverTimestampMs`) to every frame. Never use vehicle `timestampMs` for ordering or timeout logic.

**Extension codec system** — Custom vehicle protocols implement the `Codec` interface. The server routes extension payloads to registered codecs by namespace, decodes to JSON, and forwards to the UI. Unknown extensions pass through with `_error` metadata for graceful degradation.

**Zero-config deployment** — `CGO_ENABLED=0 go build` produces a single static binary (~13MB) that runs on any target without dependencies.

## Documentation

| Document | Purpose |
|----------|---------|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System topology, platform philosophy, deployment models |
| [docs/PROTOCOL.md](docs/PROTOCOL.md) | Behavioral contracts, message types, command delivery |
| [docs/EXTENSIBILITY.md](docs/EXTENSIBILITY.md) | Extension codec/manifest spec, integration contract |
| [docs/ADDING_A_VEHICLE.md](docs/ADDING_A_VEHICLE.md) | Step-by-step guide for new vehicle/protocol integration |
| [docs/NETWORKING.md](docs/NETWORKING.md) | Multicast networking explained for beginners |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | Build, run, test, simulate vehicles |
| [docs/GLOSSARY.md](docs/GLOSSARY.md) | Key term definitions (`seq`, `gts`, `HWM`, naming conventions) |
| [docs/WHY_GO.md](docs/WHY_GO.md) | Language choice justification |

## License

[MIT](LICENSE)
