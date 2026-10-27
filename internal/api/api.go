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

// Event is a captured webhook in full. Body is text, or base64 when BodyEncoding is "base64".
type Event struct {
	ID           string            `json:"id"`
	EventType    *string           `json:"event_type"`
	ReceivedAt   string            `json:"received_at"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Query        *string           `json:"query"`
	Headers      map[string]string `json:"headers"`
	Body         *string           `json:"body"`
	BodyEncoding string            `json:"body_encoding"`
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

func (c *Client) Event(id string) (Event, error) {
	var out struct {
		Event Event `json:"event"`
	}
	return out.Event, c.do(http.MethodGet, "/api/v1/events/"+url.PathEscape(id), nil, &out)
}

func (c *Client) do(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach %s: %w", c.BaseURL, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		apiErr := &Error{Status: res.StatusCode}
		_ = json.Unmarshal(data, apiErr)
		return apiErr
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
