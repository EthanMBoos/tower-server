# Why Go for tower-server

The real choice is Go vs C++. C++ is faster; this doc explains why that doesn't matter here, and what Go buys us instead.

## What this is

```
┌──────────────┐    UDP multicast    ┌──────────────┐    WebSocket     ┌──────────────┐
│  50+ Robots  │ ◀─────────────────▶ │   Server    │ ◀───────────────▶│  N Operator  │
│  10-100Hz    │   239.255.0.1:14550 │              │   localhost:9000 │     UIs      │
│  protobuf    │                     │              │   JSON frames    │              │
└──────────────┘                     └──────────────┘                  └──────────────┘
```

A protocol bridge running on operator laptops and edge devices: decode incoming protobuf, encode JSON for UIs, track fleet state, route commands.

Requirements: 5K–20K msgs/sec (50–200 vehicles × 100Hz), <10ms end-to-end, <50MB RAM, single-binary deploy across x86_64 / ARM64 / macOS.

It is *not* a flight controller, motor loop, or anything needing sub-millisecond determinism. It does not link against vehicle C++ code (standalone process). Minimum target is a Pi 4.

## The performance objections don't apply

**"C++ is faster."** True. C++ decodes protobuf at ~3.5M msgs/sec, Go at ~1.8M. We run at 5K. Both sit under 1% CPU for decode, ~4% total at 50 vehicles. The workload is IO-bound — JSON encoding and syscalls dominate, not decode — so C++'s edge moves no bottleneck.

**"GC causes latency spikes."** Our budget is 10ms (messages arrive every 10ms at 100Hz). Go's GC adds <0.5ms at p99 — a ~20x margin, and less jitter than the UDP stack itself. GC *is* disqualifying for <1ms systems like flight control; this isn't one.

## What Go actually buys us

**Single-binary deployment.** `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build` produces one ~13MB static binary with no runtime, no libc, no dependencies. The same ARM64 binary runs on a Pi 4 and a Jetson. Updating 12 laptops across 3 sites is `scp` and nothing else — no `apt`, no container pull, no Boost/protobuf/OpenSSL version matrix to reconcile per target. This is the decisive advantage for field robotics, and the one C++ can't cheaply match.

**Live production debugging.** `pprof` over HTTP attaches to a *running* field process — CPU profiles, heap dumps, goroutine stacks — with no rebuild, restart, or shipping debug symbols:

```bash
curl http://server:6060/debug/pprof/profile?seconds=30 > cpu.pprof
curl http://server:6060/debug/pprof/goroutine?debug=2
```

The C++ equivalent is recompile-with-debug-flags, redeploy, and hope you can reproduce.

**Memory safety + race detection.** No use-after-free, no buffer overflows, no segfaults. `go test -race` runs in CI on every PR and catches data races that would be silent corruption in C++. No separate ASan/TSan builds, no discipline required.

**Concurrency that matches the problem.** The server is N UDP sources → shared registry → M WebSocket clients. Goroutine-per-connection scales without thread-pool tuning or Asio strand discipline, and the runtime handles connection lifetimes.

**Tooling overhead.** `go mod download` gives hermetic, reproducible, checksummed deps. Builds are <2s vs 3–10 min for a clean C++ cross-compile, and there's no vcpkg/Conan/CMake/toolchain setup per architecture.

Feature work itself (translation, registry, sequence tracking) is comparable in both languages. Everything above is the difference.

## When to revisit

Reconsider C++ if any of these change:
- Latency tightens to <1ms (e.g. colocated flight control)
- Fleet exceeds ~500 vehicles with sub-second failover
- The server must link against existing C++ vehicle libraries
- Memory budget drops below ~10MB

None apply today.

## References

- Protobuf throughput (C++ ~2x Go): [protobuf benchmarks](https://github.com/protocolbuffers/protobuf/blob/main/benchmarks/README.md). vtprotobuf would close ~30% if it ever mattered.
- Go GC pauses (p99 <500μs): [GC guide](https://go.dev/doc/gc-guide). Verify with `GODEBUG=gctrace=1`.
- Binary size: 13MB, measured (`go build && ls -lh`). C++ static estimate 15–25MB (Boost.Asio + Beast + protobuf + OpenSSL), not measured.
- Throughput/CPU figures are estimates — profile with `go tool pprof` to confirm for your workload.