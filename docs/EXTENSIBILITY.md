# Tower Extensibility

> Extension codec/manifest spec, wire format, and integration contract.  
> For system topology and namespace governance see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## Protocol Layer: Extension Envelope

Extensions are nested inside the Pidgin envelope — payloads within `VehicleTelemetry`, not alternatives to it. See [`api/proto/pidgin.proto`](../api/proto/pidgin.proto) for the full schema (`VehicleTelemetry.extensions` map, `ExtensionData`, `ExtensionCommand`).

**Key design principles:**
- **Server core doesn't parse extension contents** — it routes versioned bytes to codecs. Server releases decouple from extension releases.
- **Per-vehicle capabilities** — `supported_extensions` in telemetry lets the UI filter to what each vehicle actually supports.
- **Independent versioning** — each extension evolves its schema independently; codecs handle version negotiation.

### Authority Chain

```
┌─────────────────────────────────────────────────────────────────────┐
│                         AUTHORITY CHAIN                             │
├─────────────────────────────────────────────────────────────────────┤
│  1. Server compiles with codec imports → availableExtensions        │
│  2. Server sends availableExtensions + manifests in welcome         │
│  3. Vehicles advertise supportedExtensions in telemetry             │
│  4. UI filters buttons by: server.available ∩ vehicle.supported     │
└─────────────────────────────────────────────────────────────────────┘
```

No config file needed. The server's registered codecs ARE the config. To disable an extension, remove its import from `cmd/tower-server/main.go`.

### Codec Discovery

Codecs self-register at startup via Go's `init()` mechanism:

```go
import _ "github.com/EthanMBoos/tower-server/internal/extensions/husky"
```

The underscore import triggers `init()`, which calls `extensions.Register()`. The server routes telemetry and commands to registered codecs by namespace.

---

## Extension Manifest

Each extension ships a `manifest.yaml` that declares its namespace and commands. The server loads all manifests at startup and includes them in the `welcome` message — the UI never reads YAML directly.

**Data flow:**
```
manifest.yaml (on disk) → server memory (startup) → welcome JSON → UI zustand store
```

YAML is the source of truth. Extension authors edit manifests without touching Go code. Manifests are static — sent once on connect, not in heartbeats. The server validates YAML at startup (fail-fast).

**Example manifest:**
```yaml
# internal/extensions/husky/manifest.yaml

namespace: husky
version: "1.0"
displayName: "Husky UGV Controls"

commands:
  - action: setDriveMode
    label: "Set Drive Mode"
    description: "Switch between manual and autonomous drive"

  - action: clearEStop
    label: "Clear E-Stop"

  - action: triggerEStop
    label: "Trigger E-Stop"
    confirmation: true
```

---

## Codec Interface

Every extension implements `Codec`. This is the contract new codec authors must satisfy:

```go
// internal/extensions/codec.go

type Codec interface {
    Namespace() string
    SupportedVersions() []uint32

    // DecodeTelemetry converts versioned proto bytes to JSON-serializable map.
    DecodeTelemetry(version uint32, data []byte) (map[string]any, error)

    // DecodeCommand converts command proto bytes to JSON-serializable map (for logging).
    DecodeCommand(action string, version uint32, data []byte) (map[string]any, error)

    // EncodeCommand converts JSON command payload to proto bytes.
    // Returns the version used for encoding (latest supported).
    EncodeCommand(action string, data map[string]any) (version uint32, payload []byte, err error)

    // Manifest returns the parsed manifest for this extension.
    Manifest() *Manifest
}
```

**Version compatibility:** Codecs MUST decode all schema versions they have ever shipped (backward compatibility). When encoding commands, use the latest version. If a vehicle sends an unrecognized version, the server passes through with `_error` metadata — telemetry continues, the UI shows a debug panel.

See [`internal/extensions/`](../internal/extensions/) for the registry implementation and [`internal/extensions/husky/`](../internal/extensions/husky/) for a reference codec.

---

## Extension Decoding

**Server always decodes extensions to JSON.** The WebSocket carries clean, human-readable JSON — no binary blobs.

| Benefit | Why |
|---------|-----|
| No protobuf runtime in browser | No bundle bloat |
| Single debug point | WebSocket traffic is readable |
| Consistent behavior | All UI clients see identical decoded data |

### Unknown Extensions

When a vehicle sends an extension the server has no codec for:

```json
"extensions": {
  "maritime": {
    "_version": 1,
    "_error": "unknown extension namespace: maritime"
  }
}
```

The rest of the telemetry frame is unaffected. The UI shows the error in a debug panel. All roads lead back to adding a codec and manifest in `internal/extensions/`.

---

## Implementation Phases

### Current (Implemented)

| Phase | Status |
|-------|--------|
| Extension envelope in proto | ✅ Done |
| Codec interface + registry | ✅ Done (`internal/extensions/`) |
| Telemetry decode via codecs | ✅ Done |
| Husky codec + manifest | ✅ Done |
| Extension commands | ✅ Done |
| Dynamic manifests in welcome | ✅ Done |
| Vehicle capabilities (supported_commands, sensors) | ✅ Done (see `pidgin.proto VehicleCapabilities`) |

### Not Implemented

| Feature | Notes |
|---------|-------|
| Manifest schema validation | YAML syntax only; no JSON Schema CI check yet (see [Open Design Gaps](#open-design-gaps)) |
| Manifest-driven UI panels | Hard-coded per extension for MVP; manifest-driven rendering is Phase 2 |
| Mission abstraction | Standard waypoint/progress format for multi-step missions |
| Coordinate frames | `Location` is implicitly WGS84; indoor/GPS-denied robots not yet supported |

---

## Open Design Gaps

### Manifest Schema Validation

> **NOT IMPLEMENTED** — Known gap before production use with multiple teams.

Currently, CI only validates YAML syntax. A malformed manifest (wrong field types, missing required fields) isn't caught until runtime. **Recommended approach:**

1. Publish a JSON Schema for `manifest.yaml` in the repo
2. Validate in CI alongside proto linting (`ajv-cli` or equivalent)
3. Server validates all manifests at startup; logs warnings but doesn't crash; only serves validated manifests

**Priority: HIGH** — A single bad PR from any team can break the UI for all operators.

### Namespace Governance CI Enforcement

> **GOVERNANCE RULES DEFINED** — CI enforcement not yet implemented.

Namespace tiers and rules are in [ARCHITECTURE.md](ARCHITECTURE.md#extension-namespace-governance). CI should reject PRs that introduce bare single-word namespaces or collide with reserved Tier 1 names.

**Priority: MEDIUM** — Enforce before onboarding third team.

---

## Integration Contract

> This is the most important architectural decision in the project.

Tower does not maintain bridges or adapters for external protocols. Teams who want to integrate must emit Pidgin proto on the multicast group.

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      YOUR SYSTEM (Team-Owned)                   │
│                                                                 │
│  Your Robot                         Your Ground Station         │
│  ┌──────────────┐  ┌─────────────┐  ┌─────────────────────┐     │
│  │  Your State  │─▶│ Radio Node  │──│  Radio Receiver     │     │
│  │  (ROS2/DDS/  │  │ + Pidgin    │  │  (passthrough)      │     │
│  │   custom)    │  │ Translation │  └────────┬────────────┘     │
│  └──────────────┘  │ (~50 lines) │           │                  │
│                    └─────────────┘           │ UDP Multicast    │
│                                              │ 239.255.0.1:14550│
└──────────────────────────────────────────────┼──────────────────┘
                                               │
┌──────────────────────────────────────────────┼──────────────────┐
│                  TOWER (We Own)              ▼                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Tower Server (speaks Pidgin proto ONLY)                 │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

Translation happens **on the robot** — in the radio node. The ground station receiver is a passthrough. This keeps the ground station generic across all robot types.

### What Tower Provides

| Asset | Description |
|-------|-------------|
| `pidgin.proto` | The protocol definition — your translation target |
| `cmd/testsender/` | Reference implementation showing how to emit telemetry |
| Field documentation | Every field explained with units and semantics |
| Server validation | Clear errors for malformed protos |

### What Teams Own

| Responsibility | Why |
|----------------|-----|
| Translation code | You know your state model — map it to `VehicleTelemetry` on the robot |
| Radio link | Your robot transmits Pidgin proto; your ground station forwards it |
| Correctness | You verify your translation emits valid protos |

**The pitch:** Add ~50 lines to your robot's radio node. Map your odometry to `VehicleTelemetry`, serialize it, and transmit. Your ground station receiver forwards to multicast. Your vehicle shows up in the UI.

See [ADDING_A_VEHICLE.md](ADDING_A_VEHICLE.md) for the step-by-step integration guide.
