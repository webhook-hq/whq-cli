package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestURLAddsPathAndQuery(t *testing.T) {
	got := URL("http://localhost:3000/", Request{Path: "stripe/events", Query: "live=1"})
	if got != "http://localhost:3000/stripe/events?live=1" {
		t.Fatal(got)
	}
}

func TestSendDeliversTheWebhookAndReportsTheAnswer(t *testing.T) {
	var gotBody, gotHeader, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody, gotHeader, gotPath = string(body), r.Header.Get("Stripe-Signature"), r.URL.Path
		w.Header().Set("X-Handled", "yes")
		w.WriteHeader(202)
		io.WriteString(w, strings.Repeat("x", BodyLimit+10))
	}))
	defer server.Close()

	result := Send(context.Background(), server.URL, Request{Method: "POST", Path: "hooks", Headers: map[string]string{"Stripe-Signature": "t=1", "Host": "in.webhookhq.dev"}, Body: []byte(`{"a":1}`)})

	if result.Status != 202 || result.Headers["X-Handled"] != "yes" || len(result.Body) != BodyLimit || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
	if gotBody != `{"a":1}` || gotHeader != "t=1" || gotPath != "/hooks" {
		t.Fatalf("app got %q %q %q", gotBody, gotHeader, gotPath)
	}
}

func TestSendExplainsNothingListening(t *testing.T) {
	result := Send(context.Background(), "http://127.0.0.1:1", Request{Method: "POST"})
	if !strings.Contains(result.Error, "nothing is listening") {
		t.Fatal(result.Error)
	}
}
