# izDeploy

izDeploy is a lightweight application deployment engine and daemon optimized for resource-constrained Linux environments (>=512MB RAM).

## Architecture Overview

The system delivers three standalone binaries compiled from a single Go module:

1. `izdeploy` (`cmd/izdeploy`): Developer CLI and Model Context Protocol (MCP) stdio server interface.
2. `izdeploy-agent` (`cmd/izdeploy-agent`): Host daemon managing container lifecycles, routing swaps, and internal health states.
3. `izdeploy-watchdog` (`cmd/izdeploy-watchdog`): Lightweight rescue process monitoring control plane connectivity and managing emergency SSH access.

## Repository Layout

```
.
├── cmd/
│   ├── izdeploy/              # CLI binary & MCP server entrypoint
│   ├── izdeploy-agent/        # Node daemon runtime
│   └── izdeploy-watchdog/     # Break-glass rescue binary
├── internal/
│   ├── platform/              # Host system primitives (UFW, systemd)
│   └── pocketbase/            # Embedded state store and event bus
├── pkg/
│   ├── contract/              # Configuration schemas and parser (.agent/izdeploy.json)
│   ├── diagnostics/           # RFC 7807 problem details classifier
│   ├── docker/                # Container runtime controller (Docker Engine SDK)
│   ├── mcp/                   # Model Context Protocol tools implementation
│   ├── proxy/                 # Zero-downtime reverse proxy controller (kamal-proxy)
│   └── tunnel/                # Reverse tunnel client (Chisel)
├── schemas/
│   └── v0.1/                  # JSON Schema specifications
├── scripts/
│   ├── setup-vps-data-plane.sh # Host bootstrap script (Docker, UFW, user)
│   ├── setup-zram.sh          # Memory optimization (zRAM LZ4, sysctl, fallback swap)
│   └── systemd/               # Systemd service and timer unit definitions
├── templates/                 # Predefined configuration and pipeline templates
└── tests/                     # Integration and end-to-end test suites
```

## System Requirements

- **Host OS**: Ubuntu 24.04 LTS (`x86_64` or `aarch64`)
- **Kernel**: Linux >= 6.8 with `cgroup2fs` unified hierarchy
- **Go Toolchain**: Go >= 1.22

## Build Instructions

Compile all target binaries:

```bash
make build
```

Run test suite:

```bash
make test
```

Execute static code analysis:

```bash
make vet
make lint
```

## Data Plane Bootstrap

Execute the initialization scripts with root privileges on target Ubuntu 24.04 instances:

```bash
# 1. Bootstrap host prerequisites, Docker runtime, user, and UFW firewall
sudo bash scripts/setup-vps-data-plane.sh

# 2. Configure zRAM swap and kernel memory parameters
sudo bash scripts/setup-zram.sh
```

## License

MIT License. See [LICENSE](LICENSE) for details.
