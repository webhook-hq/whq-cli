// Package forward sends a captured webhook to the developer's local app and reports what it
// answered.
package forward

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
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
