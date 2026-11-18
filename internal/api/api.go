// Package api talks to WebhookHQ's REST API (app.webhookhq.dev/api/v1) with an access token.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL   string
	Token     string
	UserAgent string
	HTTP      *http.Client
	// ServerTime is the Date of the last response, so waits count from WebhookHQ's clock, not this
	// computer's.
	ServerTime time.Time
}

func New(baseURL, token, userAgent string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, UserAgent: userAgent, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Error is a JSON error from the API ({"error": "...", "message": "..."}).
type Error struct {
	Status  int
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("the API answered %d", e.Status)
}

type Me struct {
	Organization struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"organization"`
	User *struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
	Token struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"token"`
}

type Endpoint struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	URL       string `json:"url"`
	Workspace string `json:"workspace"`
}

type Tunnel struct {
	ID               string   `json:"id"`
	Endpoint         Endpoint `json:"endpoint"`
	HeartbeatSeconds int      `json:"heartbeat_seconds"`
	CableURL         string   `json:"cable_url"`
	CableOrigin      string   `json:"cable_origin"`
}

// EventSummary is a captured webhook without its headers and body, as lists give it.
type EventSummary struct {
	ID          string  `json:"id"`
	EndpointID  string  `json:"endpoint_id"`
	ReceivedAt  string  `json:"received_at"`
	Method      string  `json:"method"`
	Path        string  `json:"path"`
	Query       *string `json:"query"`
	ContentType *string `json:"content_type"`
	Size        int64   `json:"size"`
	EventType   *string `json:"event_type"`
	Source      *string `json:"source"`
}

// Event is a captured webhook in full. Body is text, or base64 when BodyEncoding is "base64".
type Event struct {
	EventSummary
	Headers      map[string]string `json:"headers"`
	Body         *string           `json:"body"`
	BodyEncoding string            `json:"body_encoding"`
	SourceIP     *string           `json:"source_ip"`
	Deliveries   []Delivery        `json:"deliveries"`
}

// Delivery is an event's delivery to one destination (DestinationID nil for a tunnel).
type Delivery struct {
	ID            string  `json:"id"`
	DestinationID *string `json:"destination_id"`
	State         string  `json:"state"`
	Attempts      int     `json:"attempts"`
	LastStatus    *int    `json:"last_status"`
	NextAttemptAt *string `json:"next_attempt_at"`
	CompletedAt   *string `json:"completed_at"`
}

// EventList is a page of an endpoint's events, newest first. NextBefore pages back.
type EventList struct {
	Events     []EventSummary `json:"events"`
	HasMore    bool           `json:"has_more"`
	NextBefore *string        `json:"next_before"`
}

func (c *Client) Me() (Me, error) {
	var out Me
	return out, c.do(http.MethodGet, "/api/v1/me", nil, &out)
}

func (c *Client) OpenTunnel(endpoint, target, name string) (Tunnel, error) {
	var out struct {
		Tunnel Tunnel `json:"tunnel"`
	}
	err := c.do(http.MethodPost, "/api/v1/tunnels", map[string]string{"endpoint": endpoint, "target": target, "name": name}, &out)
	return out.Tunnel, err
}

func (c *Client) CloseTunnel(id string) error {
	return c.do(http.MethodDelete, "/api/v1/tunnels/"+url.PathEscape(id), nil, nil)
}

// Event loads one event in full, by its short or full id. Raw is the response as the API sent
// it, for --json.
func (c *Client) Event(id string) (event Event, raw []byte, err error) {
	var out struct {
		Event Event `json:"event"`
	}
	raw, err = c.get("/api/v1/events/"+url.PathEscape(id), &out)
	return out.Event, raw, err
}

// Events lists an endpoint's events with the API's filters (endpoint, type, source, status,
// range, since, before, q, limit).
func (c *Client) Events(params url.Values) (list EventList, raw []byte, err error) {
	raw, err = c.get("/api/v1/events?"+params.Encode(), &list)
	return list, raw, err
}

// Endpoints lists the endpoints the token can see, optionally in one workspace.
func (c *Client) Endpoints(workspace string) (endpoints []Endpoint, raw []byte, err error) {
	path := "/api/v1/endpoints"
	if workspace != "" {
		path += "?workspace=" + url.QueryEscape(workspace)
	}
	var out struct {
		Endpoints []Endpoint `json:"endpoints"`
	}
	raw, err = c.get(path, &out)
	return out.Endpoints, raw, err
}

func (c *Client) get(path string, out any) ([]byte, error) {
	return c.request(http.MethodGet, path, nil, out)
}

func (c *Client) do(method, path string, body any, out any) error {
	_, err := c.request(method, path, body, out)
	return err
}

func (c *Client) request(method, path string, body any, out any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach %s: %w", c.BaseURL, err)
	}
	defer res.Body.Close()
	if date, err := http.ParseTime(res.Header.Get("Date")); err == nil {
		c.ServerTime = date
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		apiErr := &Error{Status: res.StatusCode}
		_ = json.Unmarshal(data, apiErr)
		return nil, apiErr
	}
	if out == nil || len(data) == 0 {
		return data, nil
	}
	return data, json.Unmarshal(data, out)
}
