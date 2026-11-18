package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"webhookhq.dev/whq/internal/api"
	"webhookhq.dev/whq/internal/ui"
)

// waitInterval is how often whq wait checks; a variable so tests needn't wait.
var waitInterval = 2 * time.Second

// endpointFlag is --endpoint, else WHQ_ENDPOINT, else an error saying how to find one.
func endpointFlag(flags map[string]string, example string) (string, error) {
	if slug := firstNonEmpty(flags["endpoint"], os.Getenv("WHQ_ENDPOINT")); slug != "" {
		return slug, nil
	}
	return "", fmt.Errorf("which endpoint? %s (the last part of its URL; whq endpoints lists them, or set WHQ_ENDPOINT)", example)
}

// printJSON writes an API response indented, so it reads well and still parses.
func printJSON(raw []byte) error {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return err
	}
	ui.Printf("%s\n", out.String())
	return nil
}

func endpoints(args []string) error {
	_, flags, err := parseArgs(args, "workspace", "json")
	if err != nil {
		return err
	}
	c, err := client()
	if err != nil {
		return err
	}
	list, raw, err := c.Endpoints(flags["workspace"])
	if err != nil {
		return err
	}
	if flags["json"] == "true" {
		return printJSON(raw)
	}
	if len(list) == 0 {
		ui.Printf("No endpoints yet. Create one in WebhookHQ.\n")
		return nil
	}
	for _, e := range list {
		ui.Printf("%-24s %s  %s\n", ui.Bold(e.Slug), e.URL, ui.Dim(e.Name+" · "+e.Workspace))
	}
	return nil
}

func events(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("whq events list or whq events view <id>. Run whq help events")
	}
	switch args[0] {
	case "list", "ls":
		return eventsList(args[1:])
	case "view", "show":
		return eventsView(args[1:])
	default:
		return fmt.Errorf("unknown command whq events %s: it's whq events list or whq events view <id>", args[0])
	}
}

var listFilters = []string{"type", "source", "status", "range", "since", "before", "limit"}

func eventsList(args []string) error {
	_, flags, err := parseArgs(args, append(listFilters, "endpoint", "search", "q", "json")...)
	if err != nil {
		return err
	}
	slug, err := endpointFlag(flags, "whq events list --endpoint <slug>")
	if err != nil {
		return err
	}
	params := url.Values{"endpoint": {slug}, "limit": {firstNonEmpty(flags["limit"], "20")}}
	for _, name := range listFilters {
		if flags[name] != "" {
			params.Set(name, flags[name])
		}
	}
	if q := firstNonEmpty(flags["search"], flags["q"]); q != "" {
		params.Set("q", q)
	}
	c, err := client()
	if err != nil {
		return err
	}
	list, raw, err := c.Events(params)
	if err != nil {
		return err
	}
	if flags["json"] == "true" {
		return printJSON(raw)
	}
	if len(list.Events) == 0 {
		ui.Printf("No webhooks match on %s.\n", slug)
		return nil
	}
	for _, e := range list.Events {
		ui.Printf("%s  %s  %-6s %-24s %s  %s  %s\n", ui.Bold(shortID(e.ID)), ui.Dim(localTime(e.ReceivedAt)), e.Method, e.Path,
			firstNonEmpty(deref(e.EventType), "-"), firstNonEmpty(deref(e.Source), "-"), ui.Dim(size(e.Size)))
	}
	if list.HasMore && list.NextBefore != nil {
		ui.Printf("%s\n", ui.Dim("More: add --before "+*list.NextBefore))
	}
	return nil
}

func eventsView(args []string) error {
	positional, flags, err := parseArgs(args, "json", "body")
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("which webhook? whq events view <id>: the short id from whq events list, whq listen or the dashboard")
	}
	c, err := client()
	if err != nil {
		return err
	}
	event, raw, err := c.Event(positional[0])
	if err != nil {
		return err
	}
	return showEvent(event, raw, flags)
}

// showEvent prints a webhook as --json (the API's response), --body (the body alone, as sent) or
// for people: what it is, its headers, its body (JSON indented) and its deliveries.
func showEvent(event api.Event, raw []byte, flags map[string]string) error {
	switch {
	case flags["json"] == "true":
		return printJSON(raw)
	case flags["body"] == "true":
		body, err := eventBody(event)
		if err != nil {
			return err
		}
		_, err = ui.Out.Write(body)
		return err
	}

	e := event.EventSummary
	ui.Printf("%s  %s  %s\n", ui.Bold(firstNonEmpty(deref(e.EventType), "Webhook")), shortID(e.ID), ui.Dim(firstNonEmpty(deref(e.Source), "unknown sender")))
	path := e.Path
	if q := deref(e.Query); q != "" {
		path += "?" + q
	}
	ui.Printf("%s %s · %s · %s\n", e.Method, path, firstNonEmpty(deref(e.ContentType), "no content type"), size(e.Size))
	from := ""
	if ip := deref(event.SourceIP); ip != "" {
		from = " from " + ip
	}
	ui.Printf("%s\n", ui.Dim("Received "+localTime(e.ReceivedAt)+from+" · id "+e.ID))

	ui.Printf("\n%s\n", ui.Bold("Headers"))
	names := make([]string, 0, len(event.Headers))
	for name := range event.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ui.Printf("  %s: %s\n", name, event.Headers[name])
	}

	ui.Printf("\n%s\n", ui.Bold("Body"))
	switch body, err := eventBody(event); {
	case err != nil:
		return err
	case len(body) == 0:
		ui.Printf("  %s\n", ui.Dim("(empty)"))
	case event.BodyEncoding == "base64":
		ui.Printf("  %s\n", ui.Dim(fmt.Sprintf("(binary, %s: whq events view %s --body > body.bin)", size(int64(len(body))), shortID(e.ID))))
	default:
		var pretty bytes.Buffer
		if json.Indent(&pretty, body, "  ", "  ") == nil {
			body = pretty.Bytes()
		}
		ui.Printf("  %s\n", strings.TrimRight(string(body), "\n"))
	}

	ui.Printf("\n%s\n", ui.Bold("Deliveries"))
	if len(event.Deliveries) == 0 {
		ui.Printf("  %s\n", ui.Dim("None: no destinations or tunnels when it arrived."))
	}
	for _, d := range event.Deliveries {
		to := "a tunnel"
		if d.DestinationID != nil {
			to = "destination " + shortID(*d.DestinationID)
		}
		status := ""
		if d.LastStatus != nil {
			status = ", last answered " + ui.Status(*d.LastStatus)
		}
		next := ""
		if d.NextAttemptAt != nil {
			next = ui.Dim(", next try " + localTime(*d.NextAttemptAt))
		}
		ui.Printf("  %-9s to %s, %s%s%s\n", state(d.State), to, plural(d.Attempts, "attempt"), status, next)
	}
	return nil
}

func eventBody(event api.Event) ([]byte, error) {
	body := deref(event.Body)
	if event.BodyEncoding == "base64" {
		return base64.StdEncoding.DecodeString(body)
	}
	return []byte(body), nil
}

func wait(args []string) error {
	_, flags, err := parseArgs(args, "endpoint", "type", "timeout", "since", "json", "body")
	if err != nil {
		return err
	}
	slug, err := endpointFlag(flags, "whq wait --endpoint <slug>")
	if err != nil {
		return err
	}
	timeout, err := time.ParseDuration(firstNonEmpty(flags["timeout"], "2m"))
	if err != nil || timeout < 0 {
		return fmt.Errorf("--timeout %s: give a duration like 30s, 5m or 0 (no limit)", flags["timeout"])
	}
	c, err := client()
	if err != nil {
		return err
	}
	params := url.Values{"endpoint": {slug}, "limit": {"100"}}
	if flags["type"] != "" {
		params.Set("type", flags["type"])
	}

	// Count from a minute before now on WebhookHQ's clock (the Date of a first answer, else this
	// computer's), so a clock that's off can't skip a webhook, and skip the ones already there.
	seen := map[string]bool{}
	since, started := flags["since"], flags["since"]
	if since == "" {
		if _, err := c.Me(); err != nil {
			return err
		}
		now := c.ServerTime
		if now.IsZero() {
			now = time.Now()
		}
		since, started = now.Add(-time.Minute).UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339)
		params.Set("since", since)
		list, _, err := c.Events(params)
		if err != nil {
			return err
		}
		for _, e := range list.Events {
			seen[e.ID] = true
		}
	}
	params.Set("since", since)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if flags["json"] != "true" && flags["body"] != "true" {
		what := "a webhook"
		if flags["type"] != "" {
			what = "a " + flags["type"] + " webhook"
		}
		fmt.Fprintf(ui.Err, "%s\n", ui.Dim(fmt.Sprintf("Waiting for %s on %s… (Ctrl-C to stop)", what, slug)))
	}
	for {
		list, _, err := c.Events(params)
		if err != nil {
			return err
		}
		// Newest first: the first to arrive is the last new one.
		for i := len(list.Events) - 1; i >= 0; i-- {
			if id := list.Events[i].ID; !seen[id] {
				event, raw, err := c.Event(id)
				if err != nil {
					return err
				}
				return showEvent(event, raw, flags)
			}
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("nothing arrived on %s in %s. To keep waiting without missing one: whq wait --endpoint %s --since %s", slug, timeout, slug, started)
			}
			return errors.New("stopped waiting")
		case <-time.After(waitInterval):
		}
	}
}

// shortID is the 7 characters the dashboard and whq listen show.
func shortID(id string) string {
	compact := strings.ReplaceAll(id, "-", "")
	return compact[:min(7, len(compact))]
}

// localTime shows an API time in this computer's zone: the time today, or with the date before.
func localTime(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	t = t.Local()
	if y, m, d := t.Date(); y == time.Now().Year() && m == time.Now().Month() && d == time.Now().Day() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 2 15:04:05")
}

func size(bytes int64) string {
	switch {
	case bytes < 1000:
		return strconv.FormatInt(bytes, 10) + " B"
	case bytes < 1000*1000:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1000)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1000/1000)
	}
}

func state(s string) string {
	if s == "succeeded" {
		return ui.Green(s)
	}
	if s == "failed" || s == "held" {
		return ui.Red(s)
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
