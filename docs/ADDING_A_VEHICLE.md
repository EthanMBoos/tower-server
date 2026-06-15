# Integrating a New Vehicle or Protocol

How to add support for a new robot type, vehicle platform, or custom telemetry protocol to the tower-server.

There are two levels of integration depending on how different your vehicle is from the standard protocol:

1. **Standard vehicle** — your robot sends the core Pidgin protobuf schema. You only need to configure `testsender` to simulate it and define capabilities. No code changes required.
2. **Custom protocol / extension** — your robot sends additional proprietary telemetry or accepts custom commands beyond the core set. You need to implement a `Codec` and write a `manifest.yaml` in `internal/extensions/`.

---

## Level 1: Standard Vehicle Integration

If your vehicle speaks the standard Pidgin protobuf schema (`api/proto/pidgin.proto`), it works with the server out of the box. The key decisions are:

### 1. Choose an environment

| Environment | Proto enum | `-env` flag |
|---|---|---|
| Ground robot (UGV) | `ENV_GROUND` | `ground` |
| Aerial vehicle (UAV) | `ENV_AIR` | `air` |
| Marine/Surface vessel (USV) | `ENV_MARINE` | `marine` |

### 2. Define capabilities

Capabilities are advertised on every heartbeat. The server uses them to validate commands before forwarding — a command for an unsupported action is rejected at the server, not forwarded to the vehicle.

Core capability flags:

| Field | Description |
|---|---|
| `supported_commands` | List of command types: `goto`, `stop`, `return_home`, `set_mode`, `set_speed` |
| `supports_missions` | Whether the vehicle accepts waypoint mission sequences |
| `extensions` | Custom extension namespaces and their supported actions (see Level 2) |

### 3. Simulate with `testsender`

Use `testsender` to stand in for hardware during development:

```bash
# Standard UGV
go run ./cmd/testsender -vid ugv-myrobot-01 -env ground

# UAV with no stop command (e.g., fixed-wing that can't hover)
go run ./cmd/testsender -vid uav-fixedwing-01 -env air -caps no-stop

# Observation-only sensor platform (no commands)
go run ./cmd/testsender -vid sensor-mast-01 -env ground -caps none
```

The vehicle will appear in the UI's fleet view once the server receives the first heartbeat frame.

### 4. Implement the vehicle-side sender

Your vehicle firmware/software must:

1. Serialize telemetry as a `Telemetry` protobuf message (see `api/proto/pidgin.proto`)
2. Send it via UDP multicast to `239.255.0.1:14550` (configurable via `TOWER_MCAST_SOURCES`)
3. Listen for commands on `239.255.0.2:14551` (configurable via `TOWER_CMD_MCAST_GROUP` / `TOWER_CMD_MCAST_PORT`)
4. Send back a `CommandAck` frame when a command is received

Refer to `testsender` (`cmd/testsender/main.go`) as a working reference implementation.

---

## Level 2: Custom Protocol / Extension Codec

Use this when your vehicle sends proprietary data (drive mode, bumper contacts, depth readings, etc.) that doesn't fit the standard telemetry fields, or accepts custom commands beyond the core set.

An extension is a single directory under `internal/extensions/<yournamespace>/` containing two files:

```
internal/extensions/<yournamespace>/
  codec.go       — Go codec: decode telemetry, encode commands
  manifest.yaml  — platform identity, command definitions, and hardware specs
```

Both files are required. The codec handles the wire protocol; the manifest is what Tower's UI reads to display command buttons and fleet panel specs.

### How extensions work

The `Telemetry` proto message has an `extensions` map (`map<string, ExtensionData>`). Each entry is a namespace key (e.g., `"husky"`) pointing to a versioned opaque byte payload. The server passes each payload to the registered `Codec` for that namespace, which decodes it to a JSON map that gets forwarded to the UI.

Commands work in reverse: the UI sends an `extension_command` frame specifying a namespace and action, the server looks up the codec, calls `EncodeCommand`, and forwards the serialized bytes to the vehicle.

On connect, the server sends all loaded manifests in the `welcome` frame. Tower stores them in `appStore.manifests`, keyed by namespace. The fleet panel reads specs and command definitions directly from there — no client-side changes are needed to display a new extension.

### Step 1: Define your proto schema

Create `api/proto/<yournamespace>/<yournamespace>.proto`:

```proto
syntax = "proto3";
package pidgin.<yournamespace>;
option go_package = "github.com/EthanMBoos/tower-server/api/proto/<yournamespace>";

// Telemetry-specific message for your vehicle
message MyRobotTelemetry {
  string drive_mode = 1;        // e.g., "MANUAL", "AUTONOMOUS"
  bool e_stop_active = 2;
  float battery_voltage = 3;
  bool front_bumper_contact = 4;
  bool rear_bumper_contact = 5;
  // ...
}

// Command-specific messages
message SetDriveModeCommand {
  string mode = 1;  // "MANUAL" or "AUTONOMOUS"
}
```

Regenerate after editing:
```bash
protoc --go_out=. --go_opt=paths=source_relative api/proto/<yournamespace>/<yournamespace>.proto
```

### Step 2: Implement the `Codec` interface

Create `internal/extensions/<yournamespace>/codec.go`:

```go
package <yournamespace>

import (
    "fmt"

    "google.golang.org/protobuf/proto"

    "github.com/EthanMBoos/tower-server/internal/extensions"
    pb "github.com/EthanMBoos/tower-server/api/proto/<yournamespace>"
)

func init() {
    extensions.Register(&Codec{})
}

type Codec struct{}

func (c *Codec) Namespace() string { return "<yournamespace>" }

func (c *Codec) SupportedVersions() []uint32 { return []uint32{1} }

func (c *Codec) DecodeTelemetry(version uint32, data []byte) (map[string]any, error) {
    switch version {
    case 1:
        var msg pb.MyRobotTelemetry
        if err := proto.Unmarshal(data, &msg); err != nil {
            return nil, fmt.Errorf("unmarshal v1: %w", err)
        }
        // All output values must be primitive leaf types (bool, string, numeric).
        // The Tower fleet panel renderer cannot display nested maps or slices.
        // Flatten sub-messages with a key prefix rather than nesting them.
        return map[string]any{
            "driveMode":          msg.DriveMode,
            "eStopActive":        msg.EStopActive,
            "batteryVoltage":     msg.BatteryVoltage,
            "frontBumperContact": msg.FrontBumperContact,
            "rearBumperContact":  msg.RearBumperContact,
        }, nil
    default:
        return nil, fmt.Errorf("unsupported version: %d", version)
    }
}

func (c *Codec) EncodeCommand(action string, payload map[string]any) (uint32, []byte, error) {
    switch action {
    case "setDriveMode":
        mode, ok := payload["mode"].(string)
        if !ok {
            return 0, nil, fmt.Errorf("setDriveMode: missing or invalid mode")
        }
        msg := &pb.SetDriveModeCommand{Mode: mode}
        data, err := proto.Marshal(msg)
        return 1, data, err
    default:
        return 0, nil, fmt.Errorf("unknown action: %q", action)
    }
}

// SampleTelemetry implements extensions.Sampler so testsender can emit realistic
// extension payloads for this namespace during simulation.
func (c *Codec) SampleTelemetry() ([]byte, error) {
    return proto.Marshal(&pb.MyRobotTelemetry{
        DriveMode:      "AUTONOMOUS",
        EStopActive:    false,
        BatteryVoltage: 25.6,
    })
}
```

**Version compatibility contract:**
- `DecodeTelemetry` must handle all versions ever shipped — old vehicles in the field may still be sending v1 after you ship v2.
- `EncodeCommand` always encodes at the latest version.
- Return an error for unknown versions; never silently corrupt or drop data.

**Output key shape:**
- All map values must be primitive leaf types (`bool`, `string`, numeric). The Tower fleet panel renderer drops anything non-primitive.
- Use camelCase keys. Flatten sub-messages with a prefix (e.g., `"gimbalPitchDeg"`) rather than emitting nested maps.

### Step 3: Write manifest.yaml

Create `internal/extensions/<yournamespace>/manifest.yaml`. This file drives the Tower UI: command buttons come from `commands:`, and the fleet panel "Platform Specs" section comes from `specs:`.

```yaml
namespace: <yournamespace>
version: "1.0"
displayName: "My Robot Controls"

# Optional: filename of a 3D GLB in Tower's public/models/.
# Omit to use the environment default (drone.glb / ground-robot.glb / fishing_boat.glb).
# To add a custom model: drop the GLB into Tower/public/models/ and set this field.
# model: "my-robot.glb"

specs:
  - label: "Max Speed"
    value: "2.0 m/s"
  - label: "Payload"
    value: "20 kg"
  - label: "Battery"
    value: "200 Wh"

commands:
  - command: setDriveMode
    label: "Set Drive Mode"
    description: "Switch between manual and autonomous control"
    parameters:
      - name: mode
        label: "Mode"
        type: string
        required: true
        options:
          - value: "MANUAL"
            label: "Manual"
          - value: "AUTONOMOUS"
            label: "Autonomous"
```

`specs` is a flexible label/value list — include whatever is meaningful for your platform. Units go in the value string. The list is optional; omit it if there are no hardware specs to surface.

`model` is optional. When set, Tower renders that GLB from its `public/models/` directory for every vehicle of this type. When absent, the environment default is used (`drone.glb` for air, `ground-robot.glb` for ground, `fishing_boat.glb` for marine). The GLB file itself must already exist in `Tower/public/models/` — the server only carries the filename, not the binary. No Tower code changes are required; the filename travels in the welcome payload and the layer hooks resolve it at runtime.

`commands` drives the command buttons shown in the fleet panel. The `command` field must match an `action` string your `EncodeCommand` handles.

The manifest is loaded automatically at server startup from the extension directory. No registration step is needed.

### Step 4: Register the codec at startup

Import the package for its `init()` side effect in `cmd/tower-server/main.go`:

```go
import (
    // ...
    _ "github.com/EthanMBoos/tower-server/internal/extensions/<yournamespace>"
)
```

The `_` import triggers `init()`, which calls `extensions.Register()`. The server will now decode telemetry and route commands for your namespace automatically. The manifest in the same directory is loaded alongside it.

### Step 5: Advertise capabilities from the vehicle

Your vehicle's heartbeat must include extension capabilities so the server and UI know which custom commands are valid:

```proto
// In your Telemetry heartbeat:
capabilities: {
  supported_commands: ["goto", "stop"],
  extensions: [
    {
      namespace: "<yournamespace>",
      supported_actions: ["setDriveMode"]
    }
  ]
}
```

The server rejects extension commands for actions not listed here.

### Step 6: Simulate with `testsender`

`testsender` picks up your extension automatically if your codec implements the `Sampler` interface (`SampleTelemetry() ([]byte, error)`). Once implemented, run:

```bash
# In one terminal
go run ./cmd/tower-server

# In another — testsender calls SampleTelemetry() on each registered codec
go run ./cmd/testsender -vid myrobot-01 -env ground
```

The vehicle appears in Tower's fleet panel with live extension telemetry, command buttons from the manifest, and hardware specs from the `specs:` block.

### Step 7: Write a unit test for the codec

Create `internal/extensions/<yournamespace>/codec_test.go`:

```go
package <yournamespace>

import (
    "testing"

    "google.golang.org/protobuf/proto"
    pb "github.com/EthanMBoos/tower-server/api/proto/<yournamespace>"
)

func TestDecodeTelemetry(t *testing.T) {
    msg := &pb.MyRobotTelemetry{DriveMode: "AUTONOMOUS", EStopActive: false, BatteryVoltage: 25.6}
    data, _ := proto.Marshal(msg)

    c := &Codec{}
    got, err := c.DecodeTelemetry(1, data)
    if err != nil {
        t.Fatal(err)
    }
    if got["driveMode"] != "AUTONOMOUS" {
        t.Errorf("driveMode: got %v", got["driveMode"])
    }
    if got["batteryVoltage"].(float32) != 25.6 {
        t.Errorf("batteryVoltage: got %v", got["batteryVoltage"])
    }
}

func TestEncodeCommand(t *testing.T) {
    c := &Codec{}
    version, data, err := c.EncodeCommand("setDriveMode", map[string]any{"mode": "MANUAL"})
    if err != nil {
        t.Fatal(err)
    }
    if version != 1 {
        t.Errorf("expected version 1, got %d", version)
    }
    if len(data) == 0 {
        t.Error("expected non-empty encoded command")
    }
}

func TestUnknownVersion(t *testing.T) {
    c := &Codec{}
    _, err := c.DecodeTelemetry(99, []byte{})
    if err == nil {
        t.Error("expected error for unknown version")
    }
}

func TestUnknownAction(t *testing.T) {
    c := &Codec{}
    _, _, err := c.EncodeCommand("unknownAction", nil)
    if err == nil {
        t.Error("expected error for unknown action")
    }
}
```

Run with:
```bash
go test ./internal/extensions/<yournamespace>/...
```

---

## Checklist

**Standard vehicle:**
- [ ] Chose environment (`ground` / `air` / `marine`)
- [ ] Defined capabilities (supported commands, missions)
- [ ] Verified with `testsender` + `testclient`
- [ ] Vehicle firmware sends protobuf telemetry to correct multicast address
- [ ] Vehicle firmware listens for commands and sends `CommandAck`

**Custom protocol / extension:**
- [ ] Defined `.proto` schema for telemetry and command messages
- [ ] Implemented `Codec` interface (`Namespace`, `SupportedVersions`, `DecodeTelemetry`, `EncodeCommand`)
- [ ] All `DecodeTelemetry` output values are primitive leaf types (no nested maps or slices)
- [ ] Implemented `SampleTelemetry()` so `testsender` can simulate this extension
- [ ] Wrote `manifest.yaml` with `namespace`, `displayName`, `commands`, and `specs`
- [ ] If using a custom 3D model: GLB is in `Tower/public/models/` and `model:` is set in manifest.yaml
- [ ] Registered codec via blank import in `cmd/tower-server/main.go`
- [ ] Vehicle heartbeat advertises extension capabilities
- [ ] Unit tests for codec v1 round-trip, unknown version error, unknown action error
- [ ] Verified end-to-end: server + `testsender` → Tower fleet panel shows specs and command buttons
