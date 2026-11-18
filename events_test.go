package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"webhookhq.dev/whq/internal/ui"
)

// fakeAPI answers the API calls the events commands make. Events added with add show up in the
// next list, newest first, like the real API.
type fakeAPI struct {
	mu     sync.Mutex
	events []map[string]any
	plan   bool // answer lists with plan_required, as on Free
	server *httptest.Server
	lists  []string // the query of each list request
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	t.Setenv("WHQ_API_URL", f.server.URL)
	t.Setenv("WHQ_TOKEN", "whq_test")
	t.Setenv("WHQ_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("WHQ_ENDPOINT", "")
	return f
}

func (f *fakeAPI) add(id, eventType, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	event := map[string]any{"id": id, "received_at": time.Now().UTC().Format(time.RFC3339Nano), "method": "POST", "path": "/hooks",
		"size": len(body), "event_type": eventType, "source": "Stripe", "headers": map[string]string{"Content-Type": "application/json"},
		"body": body, "body_encoding": "text", "deliveries": []any{}}
	f.events = append([]map[string]any{event}, f.events...)
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	switch {
	case r.URL.Path == "/api/v1/me":
		_ = json.NewEncoder(w).Encode(map[string]any{"organization": map[string]string{"name": "Acme"}})
	case r.URL.Path == "/api/v1/events" && f.plan:
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "plan_required", "message": "The API is on the Pro plan and above."})
	case r.URL.Path == "/api/v1/events":
		f.lists = append(f.lists, r.URL.RawQuery)
		list := []map[string]any{}
		for _, e := range f.events {
			if t := r.URL.Query().Get("type"); t == "" || e["event_type"] == t {
				list = append(list, e)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"events": list, "has_more": false})
	case strings.HasPrefix(r.URL.Path, "/api/v1/events/"):
		id := strings.ReplaceAll(strings.TrimPrefix(r.URL.Path, "/api/v1/events/"), "-", "")
		for _, e := range f.events {
			if strings.HasPrefix(strings.ReplaceAll(e["id"].(string), "-", ""), id) {
				_ = json.NewEncoder(w).Encode(map[string]any{"event": e})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found", "message": "No such event, or this token can't see it."})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func capture(t *testing.T) *bytes.Buffer {
	var out bytes.Buffer
	previous := ui.Out
	ui.Out = &out
	t.Cleanup(func() { ui.Out = previous })
	return &out
}

func TestEventsListPassesFiltersAndPrintsShortIds(t *testing.T) {
	f := newFakeAPI(t)
	f.add("3f9a2c1e-0000-4000-8000-000000000000", "invoice.paid", `{}`)
	out := capture(t)
	if err := eventsList([]string{"--endpoint", "stripe", "--status", "failed", "-q", "$.id == 1", "--limit", "5"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "3f9a2c1") || !strings.Contains(out.String(), "invoice.paid") {
		t.Errorf("output: %q", out.String())
	}
	for _, want := range []string{"endpoint=stripe", "status=failed", "limit=5", "q=%24.id+%3D%3D+1"} {
		if !strings.Contains(f.lists[0], want) {
			t.Errorf("query %q lacks %s", f.lists[0], want)
		}
	}
}

func TestEventsListTakesTheEndpointFromTheEnvironment(t *testing.T) {
	f := newFakeAPI(t)
	t.Setenv("WHQ_ENDPOINT", "from-env")
	capture(t)
	if err := eventsList(nil); err != nil || !strings.Contains(f.lists[0], "endpoint=from-env") {
		t.Fatalf("%v %v", err, f.lists)
	}
}

func TestEventsListExplainsThePlan(t *testing.T) {
	f := newFakeAPI(t)
	f.plan = true
	err := eventsList([]string{"--endpoint", "stripe"})
	if err == nil || !strings.Contains(err.Error(), "whq events view and whq replay work on every plan") {
		t.Fatalf("got %v", err)
	}
}

func TestEventsViewJSONIsTheAPIResponse(t *testing.T) {
	f := newFakeAPI(t)
	f.add("3f9a2c1e-0000-4000-8000-000000000000", "invoice.paid", `{"amount":5}`)
	out := capture(t)
	if err := eventsView([]string{"3f9a2c1", "--json"}); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Event struct {
			Body string `json:"body"`
		} `json:"event"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil || parsed.Event.Body != `{"amount":5}` {
		t.Fatalf("not the API's JSON: %v %q", err, out.String())
	}
}

func TestEventsViewBodyIsExactlyAsSent(t *testing.T) {
	f := newFakeAPI(t)
	f.add("3f9a2c1e-0000-4000-8000-000000000000", "invoice.paid", `{"amount":5}`)
	out := capture(t)
	if err := eventsView([]string{"3f9a2c1", "--body"}); err != nil || out.String() != `{"amount":5}` {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestWaitShowsTheFirstWebhookAfterItStarted(t *testing.T) {
	f := newFakeAPI(t)
	waitInterval = 10 * time.Millisecond
	t.Cleanup(func() { waitInterval = 2 * time.Second })
	f.add("aaaaaaa0-0000-4000-8000-000000000000", "invoice.paid", `{"old":true}`)
	go func() {
		time.Sleep(50 * time.Millisecond)
		f.add("bbbbbbb0-0000-4000-8000-000000000000", "invoice.paid", `{"first":true}`)
		f.add("ccccccc0-0000-4000-8000-000000000000", "invoice.paid", `{"second":true}`)
	}()
	out := capture(t)
	if err := wait([]string{"--endpoint", "stripe", "--type", "invoice.paid", "--body", "--timeout", "5s"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != `{"first":true}` {
		t.Errorf("got %q, want the first new webhook", out.String())
	}
}

func TestWaitTimesOutWithTheCommandToKeepWaiting(t *testing.T) {
	newFakeAPI(t)
	waitInterval = 10 * time.Millisecond
	t.Cleanup(func() { waitInterval = 2 * time.Second })
	err := wait([]string{"--endpoint", "stripe", "--timeout", "50ms", "--json"})
	if err == nil || !strings.Contains(err.Error(), "whq wait --endpoint stripe --since 20") {
		t.Fatalf("got %v", err)
	}
}

func TestHelp(t *testing.T) {
	for _, words := range [][]string{nil, {"events"}, {"events", "view"}, {"wait"}, {"agents"}, {"replay", "extra"}} {
		if text, err := helpFor(words); err != nil || !strings.Contains(text, "whq") {
			t.Errorf("help %v: %v", words, err)
		}
	}
	if _, err := helpFor([]string{"nope"}); err == nil {
		t.Error("unknown topics should fail")
	}
	if !wantsHelp([]string{"view", "3f9a2c1", "--help"}) || wantsHelp([]string{"view"}) {
		t.Error("wantsHelp")
	}
	if got := positionalWords([]string{"view", "--json", "x"}); len(got) != 1 || got[0] != "view" {
		t.Errorf("positionalWords: %v", got)
	}
}

// Every command in the overview has its own help, so whq help <command> never fails for one.
func TestEveryCommandHasHelp(t *testing.T) {
	for _, command := range []string{"login", "whoami", "listen", "replay", "endpoints", "events list", "events view", "wait", "logout", "version"} {
		if _, ok := helps[command]; !ok {
			t.Errorf("no help for %s", command)
		}
		if !strings.Contains(usage, "  "+command) {
			t.Errorf("%s isn't in the overview", command)
		}
	}
}

func TestFormatting(t *testing.T) {
	if shortID("3f9a2c1e-0000-4000-8000-000000000000") != "3f9a2c1" || size(999) != "999 B" || size(1500) != "1.5 KB" || plural(1, "attempt") != "1 attempt" {
		t.Error("formatting")
	}
}
