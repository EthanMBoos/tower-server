# Integrating a New Vehicle or Protocol

How to add support for a new robot type or custom telemetry protocol.

You do not modify your autonomy stack. You write one translation layer that converts your robot's internal state into the Pidgin proto schema and emits it over UDP multicast. Tower and tower-server are infrastructure you run, not code you touch.

```
Your robot (your stack) -> translation layer (~50-200 lines) -> UDP multicast -> tower-server -> Tower UI
```

There are two levels. Get Level 1 working first to prove the data flow, then add Level 2 for custom telemetry and commands. Most real integrations end up at Level 2. Both levels follow `internal/extensions/husky/` and `cmd/testsender/` as working references — copy and modify rather than designing from scratch.

1. **Standard vehicle** your robot sends the core Pidgin protobuf schema. No code changes required.
2. **Custom protocol / extension** your robot sends proprietary telemetry or accepts commands beyond the core set. You need a `Codec` and a `manifest.yaml` in `internal/extensions/`.

---

## Common gotchas

- **Vehicle never appears:** check you're sending to the right multicast group/port and that `vehicle_id` is non-empty and follows `{type}-{platform}-{identifier}` (e.g. `ugv-myrobot-01`).
- **Vehicle flickers online/offline:** heartbeat or telemetry isn't arriving at least once every `TOWER_STANDBY_TIMEOUT` (default 3s).
- **Commands rejected:** the vehicle's advertised `supported_commands` (or extension `supported_actions`) must list the action. The server validates before forwarding.
- **Extension telemetry shows an `_error` field:** no codec is registered for that namespace, or the version isn't one your codec's `DecodeTelemetry` handles.

## What you don't need to do

- Touch any Tower (UI) source code. It renders entirely from what the server sends.
- Touch tower-server's core packages. Extensions are additive, registered via blank import.
- Run any of Tower's build tooling. The UI and server are independent processes connected by WebSocket.

---

## Level 1: Standard Vehicle

If your vehicle speaks the standard Pidgin protobuf schema (`api/proto/pidgin.proto`), it works out of the box.

### Choose an environment

| Environment | Proto enum | `-env` flag |
|---|---|---|
| Ground robot (UGV) | `ENV_GROUND` | `ground` |
| Aerial vehicle (UAV) | `ENV_AIR` | `air` |
| Marine/Surface vessel (USV) | `ENV_MARINE` | `marine` |

### Define capabilities

Capabilities are advertised on every heartbeat. The server rejects commands for unsupported actions before forwarding.

| Field | Description |
|---|---|
| `supported_commands` | List of command types: `goto`, `stop`, `return_home`, `set_mode`, `set_speed` |
| `supports_missions` | Whether the vehicle accepts waypoint mission sequences |
| `extensions` | Custom extension namespaces and their supported actions (Level 2) |

### Write the translation layer

On your robot (or its radio node, if translation happens off-board):

1. Map your internal state to `VehicleTelemetry` fields. Required: `vehicle_id`, `seq` (monotonic counter), `environment`, position, heading, battery.
2. Serialize as protobuf.
3. Send via UDP multicast to `239.255.0.1:14550` (configurable via `TOWER_MCAST_SOURCES`).
4. Listen on `239.255.0.2:14551` for commands; respond with `CommandAck`.

`seq` matters. The server drops out-of-order/duplicate packets by sequence number, not timestamp. Your vehicle's clock is never trusted. Start `seq` at 0 and increment on every send; restart at 0 on reboot.

See `cmd/testsender/main.go` for a working reference implementation.

### Verify the pipeline

```bash
go run ./cmd/tower-server
go run ./cmd/testclient -duration 30s
```

`testclient` prints decoded telemetry frames as they arrive. Once you see your vehicle with core fields only, the plumbing is proven. Any new bugs after this point are in your codec, not your transport.

### Simulate with `testsender`

```bash
# Standard UGV
go run ./cmd/testsender -vid ugv-myrobot-01 -env ground

# UAV with no stop command (e.g., fixed-wing that can't hover)
go run ./cmd/testsender -vid uav-fixedwing-01 -env air -caps no-stop

# Observation-only sensor platform (no commands)
go run ./cmd/testsender -vid sensor-mast-01 -env ground -caps none
```

The vehicle appears in the UI fleet view once the server receives the first heartbeat.

---

## Level 2: Custom Protocol / Extension Codec

Use this when your vehicle sends proprietary data or accepts custom commands beyond the core set.

An extension is a directory under `internal/extensions/<yournamespace>/` with two files:

```
internal/extensions/<yournamespace>/
  codec.go       decode telemetry, encode commands
  manifest.yaml  platform identity, command definitions, hardware specs
```

Both are required. The codec handles the wire protocol; the manifest is what Tower reads to display command buttons and fleet panel specs.

### How extensions work

The `Telemetry` proto message has an `extensions` map (`map<string, ExtensionData>`). Each entry is a namespace key pointing to a versioned opaque byte payload. The server passes each payload to the registered `Codec` for that namespace, which decodes it to a JSON map forwarded to the UI.

Commands work in reverse: the UI sends an `extension_command` frame with a namespace and action, the server looks up the codec, calls `EncodeCommand`, and forwards the serialized bytes to the vehicle.

On connect, the server sends all loaded manifests in the `welcome` frame. Tower stores them in `appStore.manifests` keyed by namespace. The fleet panel reads specs and command definitions from there directly.

### Step 1: Define your proto schema

Create `api/proto/<yournamespace>/<yournamespace>.proto`:

```proto
syntax = "proto3";
package pidgin.<yournamespace>;
option go_package = "github.com/EthanMBoos/tower-server/api/proto/<yournamespace>";

message MyRobotTelemetry {
  string drive_mode = 1;
  bool e_stop_active = 2;
  float battery_voltage = 3;
  bool front_bumper_contact = 4;
  bool rear_bumper_contact = 5;
}

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
        // All values must be primitive leaf types. The fleet panel drops nested maps/slices.
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
// extension payloads during simulation.
func (c *Codec) SampleTelemetry() ([]byte, error) {
    return proto.Marshal(&pb.MyRobotTelemetry{
        DriveMode:      "AUTONOMOUS",
        EStopActive:    false,
        BatteryVoltage: 25.6,
    })
}
```

Version compatibility: `DecodeTelemetry` must handle all versions ever shipped. Old vehicles may still send v1 after you ship v2. `EncodeCommand` always encodes at the latest version. Return an error for unknown versions.

Output key shape: all values must be primitive leaf types (`bool`, `string`, numeric). Use camelCase. Flatten sub-messages with a prefix rather than nesting.

### Step 3: Write manifest.yaml

Create `internal/extensions/<yournamespace>/manifest.yaml`:

```yaml
namespace: <yournamespace>
version: "1.0"
displayName: "My Robot Controls"

# Optional: GLB filename from Tower's public/models/.
# Omit to use the environment default (drone.glb / ground-robot.glb / fishing_boat.glb).
# To add a custom model, drop the GLB into Tower/public/models/ and set this field.
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

`specs` is optional. Units go in the value string. `commands` drives the command buttons in the fleet panel. The `command` field must match an `action` string your `EncodeCommand` handles.

`model` is optional. When set, Tower renders that GLB for every vehicle of this type. The file must already exist in `Tower/public/models/`.

The manifest loads automatically at server startup. No registration step needed.

### Step 4: Register the codec

Import the package for its `init()` side effect in `cmd/tower-server/main.go`:

```go
import (
    // ...
    _ "github.com/EthanMBoos/tower-server/internal/extensions/<yournamespace>"
)
```

### Step 5: Advertise capabilities from the vehicle

Your vehicle's heartbeat must include extension capabilities:

```proto
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

`testsender` picks up your extension automatically if your codec implements `SampleTelemetry()`:

```bash
go run ./cmd/tower-server
go run ./cmd/testsender -vid myrobot-01 -env ground
```

The vehicle appears in the fleet panel with live extension telemetry, command buttons, and hardware specs.

### Step 7: Write a unit test

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

```bash
go test ./internal/extensions/<yournamespace>/...
```

### Step 8: Confirm in Tower

Run the Tower UI (`npm run dev` in the Tower repo) against your running server. Your vehicle should appear in the fleet panel with live position, status, your manifest's command buttons, and your hardware specs with zero Tower code changes.

---

## Checklist

**Standard vehicle:**
- [ ] Chose environment (`ground` / `air` / `marine`)
- [ ] Defined capabilities (supported commands, missions)
- [ ] Verified with `testsender` + `testclient`
- [ ] Vehicle firmware sends protobuf telemetry to the correct multicast address
- [ ] Vehicle firmware listens for commands and sends `CommandAck`

**Custom protocol / extension:**
- [ ] Defined `.proto` schema for telemetry and command messages
- [ ] Implemented `Codec` interface (`Namespace`, `SupportedVersions`, `DecodeTelemetry`, `EncodeCommand`)
- [ ] All `DecodeTelemetry` output values are primitive leaf types
- [ ] Implemented `SampleTelemetry()` so `testsender` can simulate this extension
- [ ] Wrote `manifest.yaml` with `namespace`, `displayName`, `commands`, and `specs`
- [ ] If using a custom 3D model: GLB is in `Tower/public/models/` and `model:` is set in manifest.yaml
- [ ] Registered codec via blank import in `cmd/tower-server/main.go`
- [ ] Vehicle heartbeat advertises extension capabilities
- [ ] Unit tests for codec v1 round-trip, unknown version error, unknown action error
- [ ] Verified end-to-end: server + `testsender` shows specs and command buttons in Tower fleet panel
