package cable

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A fake Action Cable server: welcomes, confirms the subscription, sends one message and echoes
// back the first action it receives.
func TestSubscribeReceiveAndPerform(t *testing.T) {
	actions := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer whq_x" || r.Header.Get("Origin") != "http://app.test" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		ctx := r.Context()
		write := func(v any) { data, _ := json.Marshal(v); _ = ws.Write(ctx, websocket.MessageText, data) }
		write(map[string]any{"type": "welcome"})
		_, data, _ := ws.Read(ctx)
		var sub map[string]string
		_ = json.Unmarshal(data, &sub)
		write(map[string]any{"type": "confirm_subscription", "identifier": sub["identifier"]})
		write(map[string]any{"type": "ping", "message": 1})
		write(map[string]any{"identifier": sub["identifier"], "message": map[string]any{"type": "event", "delivery_id": "d1"}})
		_, data, _ = ws.Read(ctx)
		var command map[string]string
		_ = json.Unmarshal(data, &command)
		var action map[string]any
		_ = json.Unmarshal([]byte(command["data"]), &action)
		actions <- action
		for { // until the client closes, answering its close frame
			if _, _, err := ws.Read(ctx); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), "http://app.test", "whq_x", "whq/test")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Subscribe(ctx, "TunnelChannel", map[string]any{"tunnel_id": "t1"}); err != nil {
		t.Fatal(err)
	}
	message, err := conn.Receive(ctx)
	if err != nil || !strings.Contains(string(message), `"delivery_id":"d1"`) {
		t.Fatalf("%s %v", message, err)
	}
	if err := conn.Perform(ctx, "result", map[string]any{"status": 200}); err != nil {
		t.Fatal(err)
	}
	if action := <-actions; action["action"] != "result" || action["status"] != float64(200) {
		t.Fatalf("%v", action)
	}
}
