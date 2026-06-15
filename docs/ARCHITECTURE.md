# Tower Platform Architecture

> **Scope**: System-level design for the Tower platform — two repositories, one operator experience.  
> For extension codec/manifest specifics see [EXTENSIBILITY.md](EXTENSIBILITY.md).  
> For wire-format behavioral contracts see [PROTOCOL.md](PROTOCOL.md).  
> For terminology definitions see [GLOSSARY.md](GLOSSARY.md).

---

## Platform Philosophy

Tower is a **platform**, not an application. It must support different robotics projects — unmanned ground vehicles (UGV), unmanned surface vehicles (USV), and unmanned aerial vehicles (UAV) — without forking. Architecture decisions are evaluated against one question: *can a new team extend this without touching shared code?*

| Layer | Principle |
|-------|-----------|
| **Core protocol** | Position, heading, status, basic commands — universal across all vehicles |
| **Extension layer** | Custom telemetry fields, custom commands, custom UI panels per project |
| **UI & server** | One codebase — teams extend, never fork |

---

## System Topology

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                         EXTENSIONS (in-tree for MVP)                         │
│        ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐         │
│        │     husky/      │  │    skydio/      │  │   maritime/     │         │
│        │   *.proto       │  │    *.proto      │  │    *.proto      │         │
│        │   manifest.yaml │  │   manifest.yaml │  │   manifest.yaml │         │
│        │   codec.go      │  │    codec.go     │  │    codec.go     │         │
│        └─────────────────┘  └─────────────────┘  └─────────────────┘         │
└──────────────────────────────────┬───────────────────────────────────────────┘
                                   │ imported at compile time
                                   ▼
┌──────────────┐    WebSocket     ┌──────────────┐     UDP multicast     ┌──────────────┐
│  Tower       │◀────────────────▶│  Go Server   │◀─────────────────────▶│ Radio Node   │
│  (React PWA) │   localhost:9000 │              │    239.255.0.1:14550  │ (on vehicle) │
└──────────────┘                  └──────────────┘    239.255.0.2:14551  └──────────────┘
  Manifest-driven rendering         Codec registry                         Vehicle firmware
  Dynamic command flyout buttons     Proto ↔ JSON translation
  Extension state in vehicleStore   Command routing + rate limiting
```

### Transport

| Direction | Format | Transport | Address |
|-----------|--------|-----------|---------|
| Vehicle → Server | protobuf | UDP multicast | `239.255.0.1:14550` |
| Server → Vehicle | protobuf | UDP multicast | `239.255.0.2:14551` |
| Server ↔ UI | JSON | WebSocket | `ws://localhost:9000` |

**Why UDP multicast for vehicles?** Zero-infrastructure broadcast — vehicles on the same LAN receive commands without unicast routing tables. Vehicles tolerate packet loss via sequence-number deduplication, not retransmission.

**Why WebSocket for UI?** Full-duplex over TCP gives the UI reliable delivery for commands and ACKs, while still allowing the server to push high-rate telemetry.

**Why JSON on the WebSocket?** No protobuf runtime in the browser. The server decodes all binary extension payloads and hands the UI clean, human-readable JSON.

---

## Repositories

| Repo | Language | Role |
|------|----------|------|
| `tower-server` | Go | Bridges vehicles to UI; owns the wire protocol and extension registry |
| `Tower` | TypeScript/React (PWA) | Operator UI; owns rendering, command input, LLM integration |

The server has no dependency on the UI. The UI is a pure client — it never speaks directly to vehicles.

---

## Deployment Model

**Development (default):** Server and UI run on the same laptop. Vehicles are either real hardware on the LAN or simulated via `cmd/testsender`.

**Field deployment:** Server runs on a Raspberry Pi or NUC co-located with the radio hardware. The UI runs on an operator laptop on the same network. The WebSocket address is configurable via `TOWER_WS_PORT`; the UI connects to the server IP directly.

**Multi-client:** Multiple UI instances (e.g., mission commander + safety observer) can connect to one server simultaneously. Each receives the same telemetry broadcast.

---

## Security Model

**MVP: Trusted LAN only.** No authentication, TLS, or authorization.

| Future Feature | Mechanism |
|----------------|-----------|
| Auth | `Authorization` header on WebSocket upgrade (API key) |
| Encryption | `wss://` with TLS cert validation |
| Command ACL | Per-vehicle permissions keyed to client identity |

The current threat model assumes the operator network is physically controlled. Do not expose the server WebSocket port to untrusted networks.

---

## Core vs Extension Protocol Boundary

All vehicle messages are enveloped in `pidgin.proto`. Extension protos define what goes **inside** the `extensions` bytes field — nested payloads within the Pidgin envelope, not alternatives to it.

```
VehicleTelemetry (pidgin.proto)
  ├── location, speed, heading, status    ← core (typed, validated by server)
  ├── supported_extensions: ["husky"] ← capability advertisement
  └── extensions:
        "husky" → ExtensionData            ← versioned bytes, decoded by codec
              version: 1
              payload: <HuskyTelemetry proto bytes>
```

**Core absorbs universals.** If a concept applies to >2 vehicle types (sensors, missions, payloads), it belongs in `pidgin.proto` as a first-class field — not as an extension that every team must implement independently.

**Extensions own domain state.** If a concept is project-specific (drive mode, bumper contacts, gimbal pitch), it belongs in a codec with its own proto and manifest.

---

## Extension Namespace Governance

Namespace collisions are a governance problem, not just a CI check. Rules are enforced at code review:

```
TIER 1: Core Protocol  (Reserved — NOT valid extension namespaces)
  sensors, sensor, camera, mission, payload, core, pidgin

TIER 2: Domain Extensions  (team-prefixed)
  husky.drive, husky.bumpers
  maritime.depth, maritime.anchor
  agriculture.sprayer, agriculture.seeder

TIER 3: Vendor/Project Extensions  (org-prefixed)
  acme.custom_widget
  darpa.subterranean_nav
```

| Rule | Rationale |
|------|-----------|
| Core absorbs universal concepts | Sensors and missions belong in `pidgin.proto`, not extensions |
| Extensions use `domain.component` format | Ownership is unambiguous; no "camera" collision |
| No bare single-word namespaces | Exception: legacy namespaces grandfathered in |
| Org prefix for proprietary extensions | Clearly not shared platform code |

---

## Repository Strategy

For MVP, all extensions live **in-tree** under `internal/extensions/`. Splitting adds overhead that isn't justified until there are multiple contributing teams.

```
tower-server/
├── api/proto/pidgin.proto          # Core protocol
├── internal/
│   ├── extensions/
│   │   ├── registry.go             # Codec registry
│   │   ├── codec.go                # Codec interface
│   │   └── husky/                  # First extension (in-tree, MVP)
│   │       ├── husky.proto
│   │       ├── husky.pb.go
│   │       ├── codec.go
│   │       └── manifest.yaml
│   ├── protocol/
│   ├── registry/
│   └── ...
├── cmd/tower-server/
└── docs/
```

**Split triggers:**

| Trigger | Action |
|---------|--------|
| 3+ extensions with separate owners | `pidgin-extensions/` monorepo |
| External team needs independent CI | Separate repo with CODEOWNERS |
| Breaking change coordination friction | Buf.build schema registry |

---

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Protocol format (vehicle ↔ server) | protobuf over UDP multicast | Compact, no infrastructure, tolerates loss |
| Protocol format (server ↔ UI) | JSON over WebSocket | No protobuf in browser; full-duplex for commands |
| Extension encoding | proto for wire, JSON for UI | Type-safe wire; no binary in WebSocket |
| Extension validation boundary | **Both** — server rejects malformed, UI provides UX | Defense in depth |
| Manifest deployment | Static JSON (MVP) → server serves at runtime (Phase 2) | Simple first, dynamic when needed |
| Multiple namespaces per vehicle | Allowed — a vehicle can have `husky` + `camera` | Composition over inheritance |
| Unknown extensions | Fail with `_error` field, don't drop telemetry | Graceful degradation; clear integration signal |
| Timestamp authority | Server clock (`gts`) is authoritative; vehicle `ts` is untrusted | Vehicles lack RTC/NTP; clock skew is common |
| Command ordering guarantee | WebSocket in-order delivery; no retransmit | Commands are idempotent by contract |

---

## Observability (Not Implemented)

No structured observability yet. Key gaps:
- **UDP drop counters** — under load (100Hz × 10 vehicles) the kernel silently drops packets; the server has no visibility into this
- **Structured logging** — current logs are unstructured with no correlation IDs across the vehicle→server→UI path
- **Prometheus metrics** — no `/metrics` endpoint; no Grafana dashboard
