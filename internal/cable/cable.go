// Package cable speaks Rails Action Cable's protocol over a WebSocket: subscribe to a channel,
// receive its messages, and perform actions on it.
package cable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// MaxMessage is the largest message accepted: webhooks are up to 2 MB, base64 adds a third.
const MaxMessage = 8 << 20

type Conn struct {
	ws         *websocket.Conn
	identifier string
	writeMu    sync.Mutex
}

// Rejected means the server refused the subscription (closed tunnel, wrong token).
var Rejected = errors.New("subscription rejected")

type frame struct {
	Type       string          `json:"type"`
	Identifier string          `json:"identifier"`
	Message    json.RawMessage `json:"message"`
	Reason     string          `json:"reason"`
}

// Dial connects with the access token. Action Cable checks the Origin header against the app's
// URL, so it's sent as `origin`.
func Dial(ctx context.Context, url, origin, token, userAgent string) (*Conn, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("Origin", origin)
	header.Set("User-Agent", userAgent)
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to %s: %w", url, err)
	}
	ws.SetReadLimit(MaxMessage)
	return &Conn{ws: ws}, nil
}

// Subscribe joins a channel and waits until the server confirms or rejects it.
func (c *Conn) Subscribe(ctx context.Context, channel string, params map[string]any) error {
	identifier := map[string]any{"channel": channel}
	for key, value := range params {
		identifier[key] = value
	}
	encoded, err := json.Marshal(identifier)
	if err != nil {
		return err
	}
	c.identifier = string(encoded)
	if err := c.send(ctx, map[string]any{"command": "subscribe", "identifier": c.identifier}); err != nil {
		return err
	}
	for {
		f, err := c.read(ctx)
		if err != nil {
			return err
		}
		switch f.Type {
		case "confirm_subscription":
			return nil
		case "reject_subscription":
			return Rejected
		case "disconnect":
			return fmt.Errorf("the server closed the connection: %s", f.Reason)
		}
	}
}

// Receive returns the next message sent to the subscription, skipping pings.
func (c *Conn) Receive(ctx context.Context) (json.RawMessage, error) {
	for {
		f, err := c.read(ctx)
		if err != nil {
			return nil, err
		}
		if f.Type == "disconnect" {
			return nil, fmt.Errorf("the server closed the connection: %s", f.Reason)
		}
		if f.Type == "" && f.Identifier == c.identifier && len(f.Message) > 0 {
			return f.Message, nil
		}
	}
}

// Perform calls a channel action (a public method on the Rails channel) with data.
func (c *Conn) Perform(ctx context.Context, action string, data map[string]any) error {
	payload := map[string]any{"action": action}
	for key, value := range data {
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.send(ctx, map[string]any{"command": "message", "identifier": c.identifier, "data": string(encoded)})
}

func (c *Conn) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if c.identifier != "" {
		_ = c.send(ctx, map[string]any{"command": "unsubscribe", "identifier": c.identifier})
	}
	_ = c.ws.Close(websocket.StatusNormalClosure, "bye")
}

func (c *Conn) send(ctx context.Context, message map[string]any) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.Write(ctx, websocket.MessageText, encoded)
}

func (c *Conn) read(ctx context.Context) (frame, error) {
	var f frame
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		return f, err
	}
	return f, json.Unmarshal(data, &f)
}
