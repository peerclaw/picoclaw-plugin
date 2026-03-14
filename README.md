# peerclaw-picoclaw-plugin

PicoClaw channel plugin for [PeerClaw](https://github.com/peerclaw/peerclaw) — a P2P agent identity and trust platform.

This plugin implements PicoClaw's `Channel` interface using the factory registration pattern, enabling PeerClaw P2P messaging within PicoClaw's AI agent loop via a local WebSocket bridge.

## Architecture

```
PeerClaw Agent (Go)              PicoClaw
agent/platform/bridge/           this plugin
        │                            │
        ├── ws://localhost:19100 ───►│ (bridge WS server)
        │                            │
        ├── chat.send ──────────────►│──► MessageBus → AgentLoop → AI
        │◄── chat.event ────────────│◄── AI response → OutboundMessage
        ├── chat.inject ────────────►│──► notification display
        │                            │
        ▼                            ▼
    P2P Network                  PicoClaw Agent
```

The plugin starts a local WebSocket bridge server. The PeerClaw Go agent connects using the bridge adapter (`agent/platform/bridge/`). Messages flow bidirectionally:

1. **Inbound**: PeerClaw agent sends `chat.send` → plugin calls `HandleMessage()` → PicoClaw AgentLoop processes → AI response
2. **Outbound**: PicoClaw calls `Send()` → plugin sends `chat.event` frame → PeerClaw agent routes to P2P peer

## Integration

Since PicoClaw uses Go's `init()` registration pattern, integrate by adding a blank import:

```go
// In your gateway helpers or main.go:
import _ "github.com/peerclaw/picoclaw-plugin"
```

This triggers the `init()` function which calls `channels.RegisterFactory("peerclaw", ...)`.

## Configuration

Add to your PicoClaw `config.json`:

```json
{
  "channels": {
    "peerclaw": {
      "enabled": true,
      "bridge_host": "localhost",
      "bridge_port": "19100",
      "allow_from": ["peerclaw:*"]
    }
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable/disable the PeerClaw channel |
| `bridge_host` | string | `"localhost"` | Bridge WebSocket server bind address |
| `bridge_port` | string | `"19100"` | Bridge WebSocket server port |
| `allow_from` | string[] | `[]` | Allowed PeerClaw agent IDs |

## Agent-Side Setup

On the PeerClaw agent side, configure the bridge platform adapter in your `peerclaw.yaml`:

```yaml
platform:
  type: bridge
  url: "ws://localhost:19100"
```

## Bridge Protocol

Simple JSON frames over WebSocket:

**Agent → Plugin**:
```json
{"type": "chat.send", "data": {"sessionKey": "peerclaw:dm:<peer_id>", "message": "Hello"}}
{"type": "chat.inject", "data": {"sessionKey": "peerclaw:notifications", "message": "[INFO] ...", "label": "notification"}}
{"type": "ping"}
```

**Plugin → Agent**:
```json
{"type": "chat.event", "data": {"sessionKey": "peerclaw:dm:<peer_id>", "state": "final", "message": "AI response"}}
{"type": "pong"}
```

## Development

```bash
go get github.com/peerclaw/picoclaw-plugin
```

## License

Apache-2.0
