package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The local app answers over HTTPS with a certificate this computer doesn't trust (like a
// development certificate before mkcert -install): the result explains it, and --insecure
// (TrustAnyCertificate) delivers the webhook.
func TestUntrustedCertificate(t *testing.T) {
	app := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cli-destination/stripe" {
			t.Errorf("path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer app.Close()
	saved := client.Transport
	defer func() { client.Transport = saved }()

	req := Request{Method: "POST", Path: "stripe", Body: []byte(`{}`)}
	result := Send(context.Background(), app.URL+"/cli-destination", req)
	if !strings.Contains(result.Error, "doesn't trust your app's HTTPS certificate") || !strings.Contains(result.Error, "--insecure") {
		t.Fatalf("got %+v", result)
	}

	TrustAnyCertificate()
	if result := Send(context.Background(), app.URL+"/cli-destination", req); result.Status != http.StatusAccepted || result.Error != "" {
		t.Fatalf("with --insecure got %+v", result)
	}
}

func TestPlainHTTPOnAnHTTPSTarget(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer app.Close()
	target := strings.Replace(app.URL, "http://", "https://", 1)

	if result := Send(context.Background(), target, Request{Method: "POST"}); !strings.Contains(result.Error, "use http:// instead of https://") {
		t.Fatalf("got %+v", result)
	}
}

func TestRedirectHint(t *testing.T) {
	cases := []struct {
		target, location, want string
	}{
		{"http://app.webhookhq.test:3000/hooks", "https://app.webhookhq.test:3000/hooks/stripe", "use https://app.webhookhq.test:3000/hooks as the target"},
		{"http://app.test:3000", "https://app.test/stripe", "use https://app.test as the target"},
		{"http://localhost:3000", "/sign-in", "redirected the webhook to /sign-in"},
	}
	for _, c := range cases {
		got := RedirectHint(c.target, Result{Status: 301, Headers: map[string]string{"Location": c.location}})
		if !strings.Contains(got, c.want) {
			t.Errorf("RedirectHint(%q, %q) = %q; want it to contain %q", c.target, c.location, got, c.want)
		}
	}
	if got := RedirectHint("http://localhost:3000", Result{Status: 200}); got != "" {
		t.Errorf("200 got a hint: %q", got)
	}
}
