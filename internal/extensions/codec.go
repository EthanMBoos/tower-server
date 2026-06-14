package extensions

// Package extensions provides the codec interface and registry for extension namespaces.
// Each extension (e.g., "husky") registers a Codec via init() to handle
// telemetry decoding and command encoding for that namespace.

// Sampler is an optional extension to Codec. Implement it to provide realistic
// sample telemetry bytes for the testsender — keeps the testsender generic and
// platform-agnostic while each codec ships its own demo data alongside its decode logic.
type Sampler interface {
	SampleTelemetry() ([]byte, error)
}

// Codec handles encoding/decoding for a specific extension namespace.
// Register a Codec via Register() in your extension package's init() function.
//
// Version Compatibility Contract:
//   - DecodeTelemetry MUST handle all versions the codec has ever shipped.
//   - EncodeCommand always uses the latest version.
//   - If version is unrecognized, return an error; do not silently corrupt data.
type Codec interface {
	// Namespace returns the extension identifier (e.g., "husky").
	// Must be unique across all registered extensions.
	Namespace() string

	// SupportedVersions returns all schema versions this codec can decode.
	SupportedVersions() []uint32

	// DecodeTelemetry converts versioned proto bytes to a JSON-serializable map.
	// All values MUST be primitive leaf types (bool, string, numeric). Nested maps
	// and slices are silently dropped by the UI renderer (extensionFields in FleetPanel)
	// which calls formatExtValue — a function that returns null for anything non-primitive.
	// Flatten sub-messages with a key prefix (e.g., "gimbalPitchDeg") rather than
	// emitting nested structures. See husky codec for the canonical example and rationale.
	// Called by translate.go for each extension in an incoming telemetry frame.
	DecodeTelemetry(version uint32, data []byte) (map[string]any, error)

	// EncodeCommand converts a JSON command payload (from the UI) to proto bytes.
	// The action parameter is payload.type from the UI command JSON.
	// Returns the schema version used for encoding and the serialized bytes.
	EncodeCommand(action string, payload map[string]any) (version uint32, data []byte, err error)
}
