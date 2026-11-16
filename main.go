// whq is WebhookHQ's command-line tool: it delivers an endpoint's webhooks to an app on your
// machine (whq listen) and replays captured ones into it (whq replay). It authenticates with an
// access token from the dashboard (whq login); it never signs in with a password.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"webhookhq.dev/whq/internal/api"
	"webhookhq.dev/whq/internal/cable"
	"webhookhq.dev/whq/internal/config"
	"webhookhq.dev/whq/internal/forward"
	"webhookhq.dev/whq/internal/ui"
)

var version = "dev" // set by the build: -ldflags "-X main.version=1.2.3"

const usage = `whq: WebhookHQ from your terminal

Usage:
  whq login [--token TOKEN]          Save an access token (Settings → Access tokens)
  whq whoami                         Show the token's organization and owner
  whq listen [PORT|HOST:PORT|URL] --endpoint SLUG [--insecure]
                                     Deliver the endpoint's webhooks to your app (default port 3000)
  whq replay EVENT_ID [PORT|HOST:PORT|URL] [--insecure]
                                     Send a captured webhook to your app again

Targets: 3000, localhost:4000, app.test:3000, or a URL with a path,
like https://app.webhookhq.test:3000/webhooks. --insecure accepts an HTTPS
certificate this computer doesn't trust (only for your app, never for WebhookHQ).
  whq logout                         Forget the saved token
  whq version

Environment: WHQ_TOKEN (instead of whq login), WHQ_API_URL (default https://app.webhookhq.dev)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch command, args := os.Args[1], os.Args[2:]; command {
	case "login":
		err = login(args)
	case "logout":
		err = logout()
	case "whoami":
		err = whoami()
	case "listen":
		err = listen(args)
	case "replay":
		err = replay(args)
	case "version", "--version", "-v":
		fmt.Println("whq " + version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q. Run whq help", command)
	}
	if err != nil {
		ui.Errorf("%v", err)
		os.Exit(1)
	}
}

// parseArgs splits positional arguments from --flags (anywhere, as --flag value or
// --flag=value). Only the given flags are accepted.
// switches are options that take no value: --insecure sets flags["insecure"] to "true".
var switches = []string{"insecure"}

func parseArgs(args []string, known ...string) (positional []string, flags map[string]string, err error) {
	flags = map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !contains(known, name) {
			return nil, nil, fmt.Errorf("unknown option --%s", name)
		}
		if !hasValue && contains(switches, name) {
			value = "true"
		} else if !hasValue {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = args[i]
		}
		flags[name] = value
	}
	return positional, flags, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// target turns "3000", ":3000", "localhost:3000", "app.test:3000" or a URL (http or https, any
// hostname, optionally with a path) into a base URL for the local app. Each webhook's path and
// query are added after it.
func target(arg string) (string, error) {
	switch {
	case arg == "":
		return "http://localhost:3000", nil
	case strings.HasPrefix(arg, ":"):
		arg = "localhost" + arg
	}
	if port, err := strconv.Atoi(arg); err == nil {
		if port < 1 || port > 65535 {
			return "", fmt.Errorf("%d isn't a port", port)
		}
		return fmt.Sprintf("http://localhost:%d", port), nil
	}
	if !strings.Contains(arg, "://") {
		arg = "http://" + arg
	}
	u, err := url.Parse(arg)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("%q isn't a port, host:port or URL (try 3000 or https://app.test:3000/webhooks)", arg)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%q: whq sends webhooks over http:// or https:// only", arg)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q: leave out the ?query and #fragment; each webhook's own query is added", arg)
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("%q: %s isn't a port", arg, p)
		}
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.EscapedPath(), "/"), nil
}

func client() (*api.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if cfg.Token == "" {
		return nil, errors.New("no access token: run whq login, or set WHQ_TOKEN")
	}
	return api.New(cfg.APIURL, cfg.Token, "whq/"+version), nil
}

func login(args []string) error {
	_, flags, err := parseArgs(args, "token", "api")
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if flags["api"] != "" {
		cfg.APIURL = flags["api"]
	}
	token := flags["token"]
	if token == "" {
		ui.Printf("Create a token in WebhookHQ under Settings → Access tokens, then paste it here.\nToken: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		token = strings.TrimSpace(line)
	}
	if !strings.HasPrefix(token, "whq_") {
		return errors.New("that doesn't look like a WebhookHQ access token (they start with whq_)")
	}
	me, err := api.New(cfg.APIURL, token, "whq/"+version).Me()
	if err != nil {
		return err
	}
	cfg.Token = token
	if err := config.Save(cfg); err != nil {
		return err
	}
	ui.Printf("%s Signed in to %s%s.\n", ui.Green("✓"), ui.Bold(me.Organization.Name), owner(me))
	return nil
}

// owner names who a token acts as for whoami: " as Priya Raman", or nothing for a service token,
// whose name whoami already shows.
func owner(me api.Me) string {
	if me.User != nil {
		return " as " + me.User.Name
	}
	return ""
}

// closedError is WebhookHQ closing the tunnel (it went quiet, or the token lost access), as
// opposed to the connection dropping. listen reconnects either way; reopening fails cleanly when
// access is gone.
type closedError struct{ reason string }

func (e closedError) Error() string { return e.reason }

func logout() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Token = ""
	if err := config.Save(cfg); err != nil {
		return err
	}
	ui.Printf("Signed out. The token still works until you delete it in WebhookHQ.\n")
	return nil
}

func whoami() error {
	c, err := client()
	if err != nil {
		return err
	}
	me, err := c.Me()
	if err != nil {
		return err
	}
	ui.Printf("%s%s (%s token %q)\n", ui.Bold(me.Organization.Name), owner(me), me.Token.Type, me.Token.Name)
	return nil
}

func listen(args []string) error {
	positional, flags, err := parseArgs(args, "endpoint", "name", "insecure")
	if err != nil {
		return err
	}
	if flags["endpoint"] == "" {
		return errors.New("which endpoint? whq listen 3000 --endpoint <slug> (the last part of its URL)")
	}
	local, err := target(first(positional))
	if err != nil {
		return err
	}
	if flags["insecure"] == "true" {
		forward.TrustAnyCertificate()
	}
	name := flags["name"]
	if name == "" {
		name, _ = os.Hostname()
	}
	c, err := client()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	backoff, announced := time.Second, false
	for {
		tunnel, err := c.OpenTunnel(flags["endpoint"], local, name)
		var apiErr *api.Error
		if errors.As(err, &apiErr) && apiErr.Status < 500 && apiErr.Status != 429 {
			return err // not found, over the plan's limit, bad token: retrying won't help
		}
		if err == nil {
			if !announced {
				ui.Printf("%s %s %s %s\n", ui.Green("●"), ui.Bold(tunnel.Endpoint.URL), ui.Dim("→"), ui.Bold(local))
				ui.Printf("%s\n\n", ui.Dim("Waiting for webhooks. They're answered by WebhookHQ, then sent here. Ctrl-C to stop."))
				announced = true
			}
			backoff = time.Second
			err = serve(ctx, c, tunnel, local)
			_ = c.CloseTunnel(tunnel.ID) // the server also closes it when the socket drops
		}
		if ctx.Err() != nil {
			ui.Printf("\nStopped.\n")
			return nil
		}
		label := "Connection lost:"
		if errors.As(err, new(closedError)) {
			label = "Tunnel closed:"
		}
		ui.Printf("%s %s. Reconnecting in %s…\n", ui.Red(label), strings.TrimRight(err.Error(), ". "), backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

type tunnelMessage struct {
	Type       string `json:"type"`
	Reason     string `json:"reason"`
	DeliveryID string `json:"delivery_id"`
	Event      struct {
		ShortID   string            `json:"short_id"`
		EventType *string           `json:"event_type"`
		Method    string            `json:"method"`
		Path      string            `json:"path"`
		Query     string            `json:"query"`
		Headers   map[string]string `json:"headers"`
		Body      *string           `json:"body"`
	} `json:"event"`
}

// serve delivers the tunnel's webhooks, in order, until the connection or ctx ends.
func serve(ctx context.Context, c *api.Client, tunnel api.Tunnel, local string) error {
	conn, err := cable.Dial(ctx, tunnel.CableURL, tunnel.CableOrigin, c.Token, c.UserAgent)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Subscribe(ctx, "TunnelChannel", map[string]any{"tunnel_id": tunnel.ID}); err != nil {
		return err
	}

	heartbeat := time.Duration(max(tunnel.HeartbeatSeconds, 5)) * time.Second
	go func() {
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = conn.Perform(ctx, "heartbeat", nil)
			}
		}
	}()

	for {
		raw, err := conn.Receive(ctx)
		if err != nil {
			return err
		}
		var message tunnelMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			continue
		}
		switch message.Type {
		case "closed":
			return closedError{message.Reason}
		case "event":
			result := deliver(ctx, local, message)
			if err := conn.Perform(ctx, "result", map[string]any{
				"delivery_id": message.DeliveryID, "status": result.Status, "headers": result.Headers,
				"body": result.Body, "duration_ms": result.DurationMS, "error": result.Error,
			}); err != nil {
				return err
			}
		}
	}
}

func deliver(ctx context.Context, local string, message tunnelMessage) forward.Result {
	e := message.Event
	var body []byte
	if e.Body != nil {
		body, _ = base64.StdEncoding.DecodeString(*e.Body)
	}
	req := forward.Request{Method: e.Method, Path: e.Path, Query: e.Query, Headers: e.Headers, Body: body}
	result := forward.Send(ctx, local, req)
	printResult(local, e.Method, "/"+e.Path, deref(e.EventType), e.ShortID, result)
	return result
}

func printResult(local, method, path, eventType, id string, result forward.Result) {
	outcome := ui.Red(result.Error)
	if result.Error == "" {
		outcome = ui.Status(result.Status)
	}
	label := id
	if eventType != "" {
		label = eventType + " " + ui.Dim(id)
	}
	ui.Printf("%s  %-6s %-24s %s  %s %s\n", ui.Dim(time.Now().Format("15:04:05")), method, path, label, outcome,
		ui.Dim(fmt.Sprintf("%d ms", result.DurationMS)))
	if hint := forward.RedirectHint(local, result); hint != "" {
		ui.Printf("          %s\n", ui.Dim(hint))
	}
}

func replay(args []string) error {
	positional, flags, err := parseArgs(args, "port", "target", "insecure")
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("which event? whq replay <event id> [port]: the id is shown in the dashboard and by whq listen")
	}
	local, err := target(firstNonEmpty(flags["target"], flags["port"], second(positional)))
	if err != nil {
		return err
	}
	if flags["insecure"] == "true" {
		forward.TrustAnyCertificate()
	}
	c, err := client()
	if err != nil {
		return err
	}
	event, err := c.Event(positional[0])
	if err != nil {
		return err
	}
	body := []byte(deref(event.Body))
	if event.BodyEncoding == "base64" {
		if body, err = base64.StdEncoding.DecodeString(deref(event.Body)); err != nil {
			return err
		}
	}
	req := forward.Request{Method: event.Method, Path: strings.TrimPrefix(event.Path, "/"), Query: deref(event.Query),
		Headers: event.Headers, Body: body}
	result := forward.Send(context.Background(), local, req)
	printResult(local, event.Method, event.Path, deref(event.EventType), strings.ReplaceAll(event.ID, "-", "")[:7], result)
	if result.Error == "" && (result.Status < 200 || result.Status > 299) && result.Body != "" {
		ui.Printf("%s\n", ui.Dim(strings.TrimSpace(result.Body)))
	}
	return nil
}

func first(list []string) string {
	if len(list) > 0 {
		return list[0]
	}
	return ""
}

func second(list []string) string {
	if len(list) > 1 {
		return list[1]
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
