# PeerPigeon

Language: English | [中文文档](README.zh-CN.md)

PeerPigeon is a lightweight WebSocket signaling and peer discovery server, with optional hub-to-hub bootstrap for cross-network peer awareness.

## Features
- WebSocket endpoint for peer registration and signaling
- Simple HTTP endpoints for health and runtime stats
- Peer discovery broadcasts within a network namespace
- Optional hub mode with bootstrap connections to other hubs
- Minimal, dependency-light Go implementation

## Quick Start
### Requirements
- Go 1.21+ recommended

### Install and Run
```bash
go mod tidy
PORT=3000 HOST=localhost go run ./cmd/peerpigeon
```

### Check the server
```bash
curl http://localhost:3000/health
curl http://localhost:3000/stats
curl http://localhost:3000/hubstats
curl http://localhost:3000/hubs
```

## Configuration
Environment variables read at startup:
- `PORT` (default `3000`): TCP port to listen on
- `HOST` (default `localhost`): bind host
- `MAX_CONNECTIONS` (default `1000`): max concurrent WS connections
- `CORS_ORIGIN` (default `*`): CORS allow origin for HTTP endpoints
- `IS_HUB` (default `false`): enable hub mode (generates a hub peer ID)
- `HUB_MESH_NAMESPACE` (default `pigeonhub-mesh`): namespace used for hubs
- `BOOTSTRAP_HUBS` (default empty): comma-separated WebSocket URLs of other hubs (used when `IS_HUB=true`)
- `AUTH_TOKEN` (default empty): if set, WS clients must provide a bearer token

Example:
```bash
PORT=3001 HOST=0.0.0.0 IS_HUB=true BOOTSTRAP_HUBS="ws://other-host:3000/ws" \
  go run ./cmd/peerpigeon
```

## Usage
### WebSocket endpoint
- URL: `ws://<host>:<port>/ws?peerId=<40-hex>`
- The `peerId` must be a 40-character hex string.

### Authentication (optional)
If `AUTH_TOKEN` is set, clients must include either:
- Header: `Authorization: Bearer <token>`
- Query: `?token=<token>`

### Announce (join a network)
After connecting, send an `announce` message:
```json
{
  "type": "announce",
  "networkName": "global",
  "data": { "isHub": false }
}
```
Responses include `peer-discovered` messages for existing peers in the same `networkName`.

### Signaling messages
Used to exchange WebRTC-like payloads between peers:
- `offer`, `answer`, `ice-candidate`

Example:
```json
{
  "type": "offer",
  "targetPeerId": "<peer-id>",
  "networkName": "global",
  "data": { "sdp": "..." }
}
```

### Ping/Pong
Send `{"type":"ping"}` and receive `{"type":"pong"}` with a timestamp.

## HTTP API
- `GET /health`: health status and basic metrics
- `GET /stats`: runtime statistics (connections, peers, hubs, etc.)
- `GET /hubstats`: bootstrap and hub connectivity details
- `GET /hubs`: list of registered hub peers

## How It Works
### Server startup
- The server initializes a Gin engine and registers HTTP routes.
- A cleanup ticker performs periodic maintenance and relay dedup cleanup.
- If hub mode is enabled and `BOOTSTRAP_HUBS` is set, it connects to remote hubs.

Key code paths:
- Startup and routes: `internal/server/server.go:56-94`
- Cleanup ticker: `internal/server/server.go:79-85`, `internal/server/server.go:455-472`
- Port probing: `internal/server/server.go:105-115`

### WebSocket connection and auth
- `GET /ws` upgrades to WebSocket.
- If `AUTH_TOKEN` is set, clients must provide `Authorization: Bearer` or `token` query param.
- Connections are tracked in memory; peers are recorded with metadata.

Key code paths:
- Handshake and auth: `internal/server/server.go:117-158`
- Peer record updates: `internal/server/server.go:171-197`

### Announce and peer discovery
- `announce` assigns the peer to a network namespace and (optionally) marks it as a hub.
- Newly announced peers are broadcast to others via `peer-discovered`.
- A new peer receives currently active peers in its network.

Key code paths:
- Announce: `internal/server/server.go:199-231`
- Register hub: `internal/server/server.go:233-237`
- Broadcast discovery: `internal/server/server.go:239-247`
- Send existing peers to new: `internal/server/server.go:249-258`

### Local routing and signaling
- If the target is locally connected and in the same network, messages are forwarded directly.
- Otherwise, messages are relayed via bootstrap hubs (if available) with deduplication.

Key code paths:
- Local forward: `internal/server/server.go:402-405`
- Signaling handler: `internal/server/server.go:281-309`
- Relay dedup hash: `internal/server/util.go:33-37`

### Hub mode and cross-hub awareness
- In hub mode, the server generates a hub peer ID and optionally dials configured `BOOTSTRAP_HUBS`.
- On bootstrap open, the hub announces capabilities and local peers.
- Incoming `peer-discovered` from remote hubs is cached and forwarded to local peers.

Key code paths:
- Connect to bootstrap hubs: `internal/server/hubs.go:26-54`
- Announce to bootstrap and local peers: `internal/server/hubs.go:98-136`
- Handle bootstrap messages: `internal/server/hubs.go:138-167`
- Cache cross-hub peers: `internal/server/server.go:446-453`
- Forward to local peers: `internal/server/server.go:438-444`

## Testing
```bash
go test ./...
```

## Troubleshooting
- Slow dependency downloads: set a Go module proxy
```bash
go env -w GOPROXY=https://mirrors.aliyun.com/goproxy/,https://goproxy.cn,https://goproxy.io,https://proxy.golang.org,direct
```
- Port conflicts: the server probes for an available port up to `MaxPortRetries`.
- Invalid `peerId`: must be a 40-character hex string.

## PeerPigeon for JavaScript
From the legendary developer Daniel Raeder
https://github.com/PeerPigeon/PeerPigeon