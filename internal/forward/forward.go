// Package forward sends a captured webhook to the developer's local app and reports what it
// answered.
package forward

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BodyLimit is how much of the app's response is kept and reported.
const BodyLimit = 16 << 10

// Timeout is how long the local app has to answer.
const Timeout = 30 * time.Second

type Request struct {
	Method  string
	Path    string // after the endpoint URL, without a leading slash
	Query   string
	Headers map[string]string
	Body    []byte
}

type Result struct {
	Status     int               `json:"status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	DurationMS int64             `json:"duration_ms"`
	Error      string            `json:"error,omitempty"`
}

var client = &http.Client{
	Timeout:       Timeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// TrustAnyCertificate makes Send accept any HTTPS certificate from the local app (whq listen
// --insecure), for development certificates this computer doesn't trust. It only affects
// webhooks sent to the local app, never the connection to WebhookHQ.
func TrustAnyCertificate() {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the developer asked for it, for their own app
	client.Transport = transport
}

// URL is the target plus the webhook's path and query: http://localhost:3000 and "stripe" become
// http://localhost:3000/stripe.
func URL(target string, req Request) string {
	url := strings.TrimRight(target, "/")
	if req.Path != "" {
		url += "/" + strings.TrimLeft(req.Path, "/")
	}
	if req.Query != "" {
		url += "?" + req.Query
	}
	return url
}

// Send delivers req to target. Connection problems are results, not errors.
func Send(ctx context.Context, target string, req Request) Result {
	started := time.Now()
	result := func(r Result) Result { r.DurationMS = time.Since(started).Milliseconds(); return r }

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, URL(target, req), bytes.NewReader(req.Body))
	if err != nil {
		return result(Result{Error: err.Error()})
	}
	for name, value := range req.Headers {
		switch strings.ToLower(name) {
		case "host", "content-length", "connection", "transfer-encoding", "accept-encoding":
			continue
		}
		httpReq.Header.Set(name, value)
	}
	res, err := client.Do(httpReq)
	if err != nil {
		message := err.Error()
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			message = "your app didn't answer within 30 s"
		} else if strings.Contains(message, "connection refused") {
			message = "nothing is listening at " + target
		} else if untrusted := certificateProblem(err); untrusted != "" {
			message = untrusted
		} else if strings.Contains(message, "server gave HTTP response to HTTPS client") {
			message = target + " answers plain HTTP: use http:// instead of https://"
		}
		return result(Result{Error: message})
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, BodyLimit))
	headers := map[string]string{}
	for name, values := range res.Header {
		headers[name] = strings.Join(values, ", ")
	}
	return result(Result{Status: res.StatusCode, Headers: headers, Body: string(body)})
}

// certificateProblem explains an HTTPS certificate the computer doesn't accept, with what to do
// about it, or returns "" when err isn't one.
func certificateProblem(err error) string {
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verify *tls.CertificateVerificationError
	switch {
	case errors.As(err, &hostname):
		return "your app's certificate isn't for " + hostname.Host + " (" + hostname.Error() + "); use the hostname it was issued for, or --insecure"
	case errors.As(err, &unknown), errors.As(err, &invalid), errors.As(err, &verify):
		return "this computer doesn't trust your app's HTTPS certificate: if it's from mkcert, run mkcert -install; or add --insecure"
	}
	return ""
}

// RedirectHint says what to use instead when the app redirected a webhook to another URL (most
// often from http:// to https://, or to add a trailing slash), since redirected webhooks aren't
// followed: a redirect would turn a POST into a GET. Returns "" for any other result.
func RedirectHint(target string, result Result) string {
	if result.Status < 300 || result.Status > 399 {
		return ""
	}
	location := result.Headers["Location"]
	if location == "" {
		return ""
	}
	to, err := url.Parse(location)
	from, err2 := url.Parse(target)
	if err != nil || err2 != nil {
		return ""
	}
	if to.Scheme == "https" && from.Scheme == "http" && (to.Host == "" || strings.EqualFold(to.Hostname(), from.Hostname())) {
		host := to.Host
		if host == "" {
			host = from.Host
		}
		return "your app redirects to HTTPS: use https://" + host + from.Path + " as the target"
	}
	return "your app redirected the webhook to " + location + " (redirects aren't followed)"
}
