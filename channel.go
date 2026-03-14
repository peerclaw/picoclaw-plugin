// Package peerclaw implements a PicoClaw channel plugin for PeerClaw P2P
// agent identity and trust.
//
// This plugin uses PicoClaw's factory registration pattern to register
// a PeerClaw channel. It starts a local WebSocket server (bridge) that
// the PeerClaw Go agent connects to via the bridge platform adapter.
//
// Architecture:
//
//	PeerClaw Agent (Go)              PicoClaw
//	agent/platform/bridge/           this plugin
//	        │                            │
//	        ├── ws://localhost:19100 ───►│ (bridge WS server)
//	        │                            │
//	        ├── chat.send ──────────────►│──► MessageBus → AgentLoop → AI
//	        │◄── chat.event ────────────│◄── AI response → OutboundMessage
//	        ├── chat.inject ────────────►│──► notification display
//	        │                            │
package peerclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

// Bridge protocol frame types.
const (
	typeChatSend   = "chat.send"
	typeChatInject = "chat.inject"
	typeChatEvent  = "chat.event"
)

// bridgeFrame is the bridge protocol envelope.
type bridgeFrame struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type chatSendData struct {
	SessionKey string `json:"sessionKey"`
	Message    string `json:"message"`
}

type injectData struct {
	SessionKey string `json:"sessionKey"`
	Message    string `json:"message"`
	Label      string `json:"label,omitempty"`
}

type chatEventData struct {
	SessionKey string `json:"sessionKey"`
	State      string `json:"state"`
	Message    string `json:"message,omitempty"`
}

// Channel implements the PicoClaw channels.Channel interface for PeerClaw.
type Channel struct {
	*channels.BaseChannel
	cfg        *config.Config
	logger     *slog.Logger
	httpServer *http.Server
	clients    map[*websocket.Conn]struct{}
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewChannel creates a new PeerClaw channel.
func NewChannel(cfg *config.Config, msgBus *bus.MessageBus) (*Channel, error) {
	base := channels.NewBaseChannel("peerclaw", cfg, msgBus)
	return &Channel{
		BaseChannel: base,
		cfg:         cfg,
		logger:      slog.Default().With("channel", "peerclaw"),
		clients:     make(map[*websocket.Conn]struct{}),
	}, nil
}

// Name returns the channel identifier.
func (c *Channel) Name() string { return "peerclaw" }

// Start begins the bridge WebSocket server.
func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	host := c.GetConfigString("bridge_host", "localhost")
	port := c.GetConfigString("bridge_port", "19100")
	addr := net.JoinHostPort(host, port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", c.handleWebSocket)

	c.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	c.SetRunning(true)
	c.logger.Info("PeerClaw bridge listening", "addr", addr)

	if err := c.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		c.SetRunning(false)
		return fmt.Errorf("bridge server: %w", err)
	}
	return nil
}

// Stop shuts down the bridge server.
func (c *Channel) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	if c.httpServer != nil {
		_ = c.httpServer.Shutdown(ctx)
	}

	c.mu.Lock()
	for conn := range c.clients {
		conn.Close(websocket.StatusNormalClosure, "shutting down")
		delete(c.clients, conn)
	}
	c.mu.Unlock()

	c.SetRunning(false)
	c.logger.Info("PeerClaw bridge stopped")
	return nil
}

// Send delivers an AI response back to the PeerClaw agent via bridge.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	evt := chatEventData{
		SessionKey: "peerclaw:dm:" + msg.ChatID,
		State:      "final",
		Message:    msg.Content,
	}
	evtJSON, _ := json.Marshal(evt)
	frame := bridgeFrame{
		Type: typeChatEvent,
		Data: evtJSON,
	}
	data, _ := json.Marshal(frame)

	c.mu.Lock()
	defer c.mu.Unlock()

	for conn := range c.clients {
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			conn.Close(websocket.StatusAbnormalClosure, "write error")
			delete(c.clients, conn)
		}
	}
	return nil
}

func (c *Channel) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Local bridge only.
	})
	if err != nil {
		c.logger.Error("WebSocket accept failed", "error", err)
		return
	}
	conn.SetReadLimit(256 * 1024)

	c.mu.Lock()
	c.clients[conn] = struct{}{}
	c.mu.Unlock()

	c.logger.Info("PeerClaw agent connected")

	defer func() {
		c.mu.Lock()
		delete(c.clients, conn)
		c.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
		c.logger.Info("PeerClaw agent disconnected")
	}()

	for {
		_, data, err := conn.Read(c.ctx)
		if err != nil {
			return
		}
		c.handleFrame(data)
	}
}

func (c *Channel) handleFrame(data []byte) {
	var frame bridgeFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		c.logger.Warn("invalid bridge frame", "error", err)
		return
	}

	switch frame.Type {
	case typeChatSend:
		var d chatSendData
		if err := json.Unmarshal(frame.Data, &d); err != nil {
			c.logger.Warn("invalid chat.send data", "error", err)
			return
		}
		senderID := extractPeerID(d.SessionKey)
		c.HandleMessage(
			c.ctx,
			bus.Peer{Kind: "direct", ID: senderID},
			"",
			senderID,
			senderID,
			d.Message,
			nil, nil,
			bus.SenderInfo{
				Platform:    "peerclaw",
				PlatformID:  senderID,
				CanonicalID: "peerclaw:" + senderID,
			},
		)

	case typeChatInject:
		var d injectData
		if err := json.Unmarshal(frame.Data, &d); err != nil {
			c.logger.Warn("invalid chat.inject data", "error", err)
			return
		}
		c.logger.Info("PeerClaw notification", "label", d.Label, "message", d.Message)
		c.HandleMessage(
			c.ctx,
			bus.Peer{Kind: "direct", ID: "peerclaw-notifications"},
			"",
			"peerclaw-system",
			"peerclaw-notifications",
			d.Message,
			nil, nil,
			bus.SenderInfo{
				Platform:    "peerclaw",
				PlatformID:  "system",
				CanonicalID: "peerclaw:system",
				DisplayName: "PeerClaw",
			},
		)

	case "ping":
		pong := bridgeFrame{Type: "pong"}
		pongData, _ := json.Marshal(pong)
		c.mu.Lock()
		for conn := range c.clients {
			_ = conn.Write(c.ctx, websocket.MessageText, pongData)
		}
		c.mu.Unlock()
	}
}

func extractPeerID(sessionKey string) string {
	const prefix = "peerclaw:dm:"
	if strings.HasPrefix(sessionKey, prefix) {
		return sessionKey[len(prefix):]
	}
	return sessionKey
}
