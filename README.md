# Google Bandwidth Controller

A Go-based bandwidth traffic controller system designed to manage 15 VPS servers generating Google download traffic, targeting 10Gbps total bandwidth with natural, non-linear traffic patterns for Google PNI application.

## Features

- **Dynamic Traffic Scheduling**: Random rotation with 2-8 concurrent servers
- **Natural Traffic Patterns**: Sine wave algorithms create organic-looking bandwidth variations
- **Real-time Monitoring**: HTTP API and console dashboard for bandwidth tracking
- **Auto-reconnection**: Agents automatically reconnect on disconnection
- **Bandwidth Control**: Precise control using wget with rate limiting
- **Scalable Architecture**: Easy to add/remove servers from the pool

## Architecture

```
┌─────────────────────────────────────────┐
│          Controller (Master)             │
│  ┌────────────┐  ┌──────────────────┐   │
│  │ WebSocket  │  │  HTTP API        │   │
│  │ Server     │  │  (Metrics)       │   │
│  │ :8080      │  │  :9090           │   │
│  └────────────┘  └──────────────────┘   │
│  ┌────────────────────────────────────┐ │
│  │  Scheduler (Random Rotation)       │ │
│  │  - Dynamic concurrency (2-8)       │ │
│  │  - Weighted agent selection        │ │
│  │  - Bandwidth allocation            │ │
│  └────────────────────────────────────┘ │
└─────────────────────────────────────────┘
           │     │     │
      WebSocket  │     │  (15 connections)
           │     │     │
    ┌──────┴─┬───┴──┬──┴────┐
    │        │      │       │
┌───▼───┐ ┌─▼────┐ ┌▼─────┐ ...
│Agent 1│ │Agent2│ │Agent3│ (15 total)
│ wget  │ │ wget │ │ wget │
└───────┘ └──────┘ └──────┘
    ↓        ↓        ↓
  Google   Google   Google
```

## Requirements

- **Go**: 1.21 or higher
- **wget**: Installed on all agent machines
- **Network**: All agents must be able to connect to controller
- **Bandwidth**: VPS servers with sufficient bandwidth to Google

## Quick Start

### 1. Build Binaries

```bash
# Build controller
go build -o controller ./cmd/controller

# Build agent
go build -o agent ./cmd/agent
```

### 2. Deploy Controller

```bash
# On your master controller server
sudo ./scripts/deploy-controller.sh

# Edit configuration
sudo nano /etc/bandwidth-controller/controller.yaml

# Update:
# - server.auth_token (change to a secure token)
# - agents[] (configure all 15 VPS hosts)
# - download_urls[] (add Google service URLs)

# Start controller
sudo systemctl enable bandwidth-controller
sudo systemctl start bandwidth-controller

# View dashboard
sudo journalctl -u bandwidth-controller -f
```

### 3. Deploy Agents

On each of your 15 VPS servers:

```bash
# Copy agent binary to VPS
scp agent user@vps1.example.com:~/

# SSH to VPS
ssh user@vps1.example.com

# Deploy agent
sudo ./deploy-agent.sh agent-001 "VPS-Tokyo-1" controller.example.com YOUR_AUTH_TOKEN

# Start agent
sudo systemctl enable bandwidth-agent
sudo systemctl start bandwidth-agent

# Check status
sudo systemctl status bandwidth-agent
```

Repeat for all 15 servers with unique agent IDs (agent-001 through agent-015).

## Configuration

### Controller Configuration

See [configs/controller.yaml](configs/controller.yaml) for full configuration options.

Key settings:

```yaml
bandwidth:
  target_gbps: 10.0          # Target total bandwidth

scheduler:
  min_concurrent: 2           # Minimum active servers
  max_concurrent: 8           # Maximum active servers
  rotation_interval_min: 30s  # Min time between rotations
  rotation_interval_max: 180s # Max time between rotations

agents:
  - id: "agent-001"
    host: "vps1.example.com"
    name: "VPS-Tokyo-1"
    max_bandwidth: 1500       # Mbps
    region: "tokyo"
```

### Agent Configuration

See [configs/agent.yaml](configs/agent.yaml) for full configuration.

Each agent needs:
- Unique `agent.id` matching controller config
- Controller hostname and auth token
- wget installed on the system

## Monitoring

### Console Dashboard

The controller displays a real-time dashboard:

```
═══════════════════════════════════════════════════════════════
          Google Bandwidth Controller Dashboard
═══════════════════════════════════════════════════════════════

Target: 10.00 Gbps | Current: 9.85 Gbps (98.5%)
Active Agents: 5/15
Next Rotation: 14:32:15 (45s)
Phase: stable | Rotations: 127

Agent Bandwidth Breakdown:
─────────────────────────────────────────────────────────────
VPS-Tokyo-1          [████████████░░░░░░░░]    1250 Mbps
VPS-Singapore-1      [██████████░░░░░░░░░░]    1050 Mbps
VPS-Seoul-2          [███████████████░░░░░]    1550 Mbps
VPS-LA-1             [█████████░░░░░░░░░░░]     950 Mbps
VPS-Taiwan-1         [████████████████░░░░]    1600 Mbps

═══════════════════════════════════════════════════════════════
Metrics API: http://0.0.0.0:9090/metrics
═══════════════════════════════════════════════════════════════
```

### HTTP API

The controller exposes a monitoring API on port 9090:

**Get Current Metrics:**
```bash
curl http://controller:9090/metrics | jq
```

Response:
```json
{
  "total_bandwidth_mbps": 9850.5,
  "total_bandwidth_gbps": 9.85,
  "active_agents": 5,
  "total_agents": 15,
  "agent_breakdown": {
    "agent-001": 1250.3,
    "agent-003": 1050.8,
    ...
  },
  "target_bandwidth_gbps": 10.0,
  "target_percentage": 98.5
}
```

**Get System Status:**
```bash
curl http://controller:9090/status | jq
```

**Get Agent List:**
```bash
curl http://controller:9090/agents | jq
```

**Get Historical Data:**
```bash
curl http://controller:9090/history?duration=1h | jq
```

**Get Statistics:**
```bash
curl http://controller:9090/stats?duration=24h | jq
```

## How It Works

### Scheduling Algorithm

The controller uses a sophisticated scheduling algorithm to create natural traffic patterns:

1. **Dynamic Concurrency**: Uses overlapping sine waves to vary the number of active servers (2-8)
   - 5-minute, 3-minute, and 7-minute wave cycles combined
   - Creates complex, organic patterns

2. **Weighted Random Selection**: Selects which agents to use based on:
   - Server capacity (max bandwidth)
   - Last usage time (prefer idle servers)
   - Geographic distribution (avoid region clustering)

3. **Unequal Bandwidth Allocation**:
   - Generates random weights for each active server
   - Distributes 10Gbps proportionally with variance
   - Adds ±10% random variation per server

4. **Gradual Transitions**:
   - Ramp down: Stagger stop commands over 20 seconds
   - Ramp up: Stagger start commands over 15 seconds with jitter
   - Prevents sudden bandwidth spikes

5. **Random Rotation Timing**:
   - Rotation intervals: 30-180 seconds
   - Additional jitter applied for unpredictability

### Traffic Pattern Characteristics

- Total bandwidth oscillates around 10Gbps (±15%)
- Number of active servers varies continuously
- Individual server bandwidth: 400-1200 Mbps
- No straight lines - continuous natural variation
- Geographic diversity maintained

## Troubleshooting

### Controller Not Starting

```bash
# Check logs
sudo journalctl -u bandwidth-controller -n 50

# Common issues:
# 1. Port already in use (8080 or 9090)
# 2. Configuration validation failed
# 3. Invalid auth_token or agent config
```

### Agent Not Connecting

```bash
# Check agent logs
sudo journalctl -u bandwidth-agent -n 50

# Common issues:
# 1. Controller host unreachable
# 2. Firewall blocking port 8080
# 3. Auth token mismatch
# 4. wget not installed
```

### Low Bandwidth

```bash
# Check current metrics
curl http://controller:9090/metrics

# Possible causes:
# 1. Not enough agents connected
# 2. Agents hitting max_bandwidth limits
# 3. Network bottlenecks on VPS
# 4. Google rate limiting
```

### Bandwidth Too High

```bash
# Lower target in controller.yaml:
bandwidth:
  target_gbps: 8.0  # Instead of 10.0

# Or reduce max concurrent:
scheduler:
  max_concurrent: 6  # Instead of 8
```

## Production Deployment

### Security Considerations

1. **Change Auth Token**: Use a strong, random token
   ```bash
   openssl rand -base64 32
   ```

2. **Firewall Rules**:
   - Controller: Allow port 8080 from agent IPs only
   - Controller: Allow port 9090 from monitoring systems only

3. **TLS/SSL**: Consider adding TLS for WebSocket connections in production

4. **Resource Limits**: Monitor CPU, memory, and disk usage

### Performance Tuning

For optimal performance:

1. **Increase file descriptor limits** on all servers:
   ```bash
   ulimit -n 65536
   ```

2. **Tune network stack** for high bandwidth:
   ```bash
   sysctl -w net.core.rmem_max=134217728
   sysctl -w net.core.wmem_max=134217728
   ```

3. **Monitor disk I/O** if not cleaning up temp files

### Monitoring

Set up external monitoring:

1. **Prometheus Integration**: Add prometheus metrics endpoint
2. **Alerting**: Set up alerts for:
   - Agents disconnected > 5 minutes
   - Total bandwidth < 8.5 Gbps for > 10 minutes
   - Controller crashes

## Development

### Project Structure

```
google-bandwidth-controller/
├── cmd/
│   ├── controller/     # Controller entry point
│   └── agent/          # Agent entry point
├── internal/
│   ├── controller/     # Controller logic
│   ├── agent/          # Agent logic
│   ├── protocol/       # WebSocket protocol
│   └── bandwidth/      # Bandwidth utilities
├── pkg/
│   └── logger/         # Logging wrapper
├── configs/            # Configuration templates
├── deployments/        # Systemd services
└── scripts/            # Deployment scripts
```

### Building

```bash
# Build both binaries
make build

# Or individually
go build -o controller ./cmd/controller
go build -o agent ./cmd/agent

# Build for Linux (if developing on Mac)
GOOS=linux GOARCH=amd64 go build -o controller-linux ./cmd/controller
GOOS=linux GOARCH=amd64 go build -o agent-linux ./cmd/agent
```

### Testing

```bash
# Run tests
go test ./...

# Test with local agent
./controller -config configs/controller.yaml &
./agent -config configs/agent.yaml
```

## FAQ

**Q: Can I use fewer than 15 agents?**
A: Yes, the system works with any number of agents. Update `agents[]` in controller.yaml.

**Q: Can I change the target bandwidth?**
A: Yes, modify `bandwidth.target_gbps` in controller.yaml and restart the controller.

**Q: What if an agent crashes?**
A: The scheduler automatically redistributes bandwidth to remaining agents. The crashed agent will auto-reconnect.

**Q: Can I add more download URLs?**
A: Yes, add URLs to `download_urls[]` in controller.yaml. No restart needed.

**Q: Does this work with IPv6?**
A: Yes, as long as wget supports it and URLs are accessible via IPv6.

## License

MIT License - See LICENSE file for details

## Support

For issues or questions:
1. Check logs: `journalctl -u bandwidth-controller -f`
2. Check metrics API: `curl http://controller:9090/metrics`
3. Review configuration files
4. Ensure wget is installed and functional on all agents

## Credits

Built for Google PNI bandwidth requirements with natural traffic pattern generation.
