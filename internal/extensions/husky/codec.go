// Package husky provides the extension codec for Clearpath Husky A200 UGV.
//
// This codec decodes Husky-specific telemetry and encodes commands.
// It registers itself via init() — import this package in cmd/tower-server/main.go
// to enable Husky support.
//
// Usage:
//
//	import _ "github.com/EthanMBoos/tower-server/internal/extensions/husky"
package husky

import (
	"errors"
	"fmt"

	"github.com/EthanMBoos/tower-server/internal/extensions"
	"google.golang.org/protobuf/proto"
)

func init() {
	extensions.Register(&Codec{})
}

// Codec implements extensions.Codec for Husky UGV.
type Codec struct{}

var _ extensions.Codec = (*Codec)(nil) // Compile-time interface check

func (c *Codec) Namespace() string { return "husky" }

func (c *Codec) SupportedVersions() []uint32 { return []uint32{1} }

// DecodeTelemetry converts HuskyTelemetry proto bytes to JSON-serializable map.
//
// All output keys are flat primitives. The proto uses a BumperContacts sub-message
// for semantic grouping, but we flatten it here so the UI's extensionFields()
// renderer — which only handles primitive leaf values — can display every field
// without special-casing nested maps. Schema stays idiomatic; adaptation happens
// at this codec boundary.
func (c *Codec) DecodeTelemetry(version uint32, data []byte) (map[string]any, error) {
	if version != 1 {
		return nil, fmt.Errorf("unsupported husky telemetry version: %d", version)
	}

	var msg HuskyTelemetry
	if err := proto.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal husky telemetry: %w", err)
	}

	result := map[string]any{
		"driveMode":       driveModeToString(msg.DriveMode),
		"batteryVoltage":  msg.BatteryVoltage,
		"motorTempLeftC":  msg.MotorTempLeftC,
		"motorTempRightC": msg.MotorTempRightC,
		"odometryLeftM":   msg.OdometryLeftM,
		"odometryRightM":  msg.OdometryRightM,
		"estopEngaged":    msg.EstopEngaged,
	}

	if msg.BumperContacts != nil {
		result["bumperFrontLeft"]  = msg.BumperContacts.FrontLeft
		result["bumperFrontRight"] = msg.BumperContacts.FrontRight
		result["bumperRearLeft"]   = msg.BumperContacts.RearLeft
		result["bumperRearRight"]  = msg.BumperContacts.RearRight
	}

	return result, nil
}

// EncodeCommand converts a UI command payload to proto bytes.
func (c *Codec) EncodeCommand(action string, payload map[string]any) (uint32, []byte, error) {
	switch action {
	case "setDriveMode":
		return c.encodeSetDriveMode(payload)
	case "setBumperSensitivity":
		return c.encodeSetBumperSensitivity(payload)
	case "triggerEStop":
		return c.encodeTriggerEStop(payload)
	case "releaseEStop":
		return c.encodeReleaseEStop(payload)
	default:
		return 0, nil, fmt.Errorf("unknown husky action: %s", action)
	}
}

func (c *Codec) encodeSetDriveMode(payload map[string]any) (uint32, []byte, error) {
	modeStr, ok := payload["mode"].(string)
	if !ok {
		return 0, nil, errors.New("missing or invalid 'mode' field")
	}

	mode, err := stringToDriveMode(modeStr)
	if err != nil {
		return 0, nil, err
	}

	msg := &SetDriveModeCommand{Mode: mode}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetDriveModeCommand: %w", err)
	}

	return 1, data, nil
}

func (c *Codec) encodeSetBumperSensitivity(payload map[string]any) (uint32, []byte, error) {
	// JSON numbers come as float64
	sensFloat, ok := payload["sensitivity"].(float64)
	if !ok {
		return 0, nil, errors.New("missing or invalid 'sensitivity' field")
	}

	msg := &SetBumperSensitivityCommand{Sensitivity: uint32(sensFloat)}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetBumperSensitivityCommand: %w", err)
	}

	return 1, data, nil
}

func (c *Codec) encodeTriggerEStop(_ map[string]any) (uint32, []byte, error) {
	msg := &TriggerEStopCommand{}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal TriggerEStopCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeReleaseEStop(_ map[string]any) (uint32, []byte, error) {
	msg := &ReleaseEStopCommand{}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal ReleaseEStopCommand: %w", err)
	}
	return 1, data, nil
}

// ── Enum Helpers ──

func driveModeToString(m DriveMode) string {
	switch m {
	case DriveMode_DRIVE_MODE_MANUAL:
		return "manual"
	case DriveMode_DRIVE_MODE_AUTONOMOUS:
		return "autonomous"
	case DriveMode_DRIVE_MODE_GUIDED:
		return "guided"
	default:
		return "unknown"
	}
}

func stringToDriveMode(s string) (DriveMode, error) {
	switch s {
	case "manual":
		return DriveMode_DRIVE_MODE_MANUAL, nil
	case "autonomous":
		return DriveMode_DRIVE_MODE_AUTONOMOUS, nil
	case "guided":
		return DriveMode_DRIVE_MODE_GUIDED, nil
	default:
		return DriveMode_DRIVE_MODE_UNKNOWN, fmt.Errorf("unknown drive mode: %s", s)
	}
}
