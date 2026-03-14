[English](README.md) | **中文**

# peerclaw-picoclaw-plugin

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

[PeerClaw](https://github.com/peerclaw/peerclaw) 的 PicoClaw 通道插件 — 一个 P2P 智能体身份与信任平台。

本插件使用工厂注册模式实现了 PicoClaw 的 `Channel` 接口，通过本地 WebSocket 桥接在 PicoClaw 的 AI 智能体循环中启用 PeerClaw P2P 消息通信。

## 架构

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

插件启动一个本地 WebSocket 桥接服务器。PeerClaw Go 智能体使用桥接适配器（`agent/platform/bridge/`）进行连接。消息双向流动：

1. **入站**：PeerClaw 智能体发送 `chat.send` → 插件调用 `HandleMessage()` → PicoClaw AgentLoop 处理 → AI 响应
2. **出站**：PicoClaw 调用 `Send()` → 插件发送 `chat.event` 帧 → PeerClaw 智能体将消息路由至 P2P 对等节点

## 集成

由于 PicoClaw 使用 Go 的 `init()` 注册模式，通过添加空白导入即可集成：

```go
// In your gateway helpers or main.go:
import _ "github.com/peerclaw/picoclaw-plugin"
```

这会触发 `init()` 函数，调用 `channels.RegisterFactory("peerclaw", ...)`。

## 配置

在 PicoClaw 的 `config.json` 中添加：

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

### 配置选项

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `false` | 启用/禁用 PeerClaw 通道 |
| `bridge_host` | string | `"localhost"` | 桥接 WebSocket 服务器绑定地址 |
| `bridge_port` | string | `"19100"` | 桥接 WebSocket 服务器端口 |
| `allow_from` | string[] | `[]` | 允许的 PeerClaw 智能体 ID 列表 |

## 智能体端配置

在 PeerClaw 智能体端，在 `peerclaw.yaml` 中配置桥接平台适配器：

```yaml
platform:
  type: bridge
  url: "ws://localhost:19100"
```

## 桥接协议

基于 WebSocket 的简单 JSON 帧：

**智能体 → 插件**：
```json
{"type": "chat.send", "data": {"sessionKey": "peerclaw:dm:<peer_id>", "message": "Hello"}}
{"type": "chat.inject", "data": {"sessionKey": "peerclaw:notifications", "message": "[INFO] ...", "label": "notification"}}
{"type": "ping"}
```

**插件 → 智能体**：
```json
{"type": "chat.event", "data": {"sessionKey": "peerclaw:dm:<peer_id>", "state": "final", "message": "AI response"}}
{"type": "pong"}
```

## 开发

```bash
go get github.com/peerclaw/picoclaw-plugin
```

## 许可证

[Apache-2.0](LICENSE)
