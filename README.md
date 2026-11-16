# whq: WebhookHQ from your terminal

`whq` delivers an endpoint's webhooks to an app running on your machine, and replays captured webhooks into it. It's built only for webhooks: it isn't a general tool for putting websites on the internet.

## Sign in

Create an access token in WebhookHQ under **Settings → Access tokens** (a personal token acts as you; a service token, made by owners and admins, suits CI), then:

```sh
whq login            # paste the token when asked
whq whoami           # Acme as Priya Raman (personal token "MacBook")
```

The token is saved in `~/.config/whq/config.json`, readable only by you. In CI, set `WHQ_TOKEN` instead. `whq logout` forgets it (the token keeps working until you delete it in WebhookHQ).

## Receive webhooks on your machine

```sh
whq listen 3000 --endpoint acme-stripe-test
```

```
● https://in.webhookhq.dev/acme/acme-stripe-test → http://localhost:3000
Waiting for webhooks. They're answered by WebhookHQ, then sent here. Ctrl-C to stop.

10:42:07  POST   /webhooks   checkout.session.completed 3f9a2c1  200 14 ms
10:42:31  POST   /webhooks   invoice.payment_failed 8be04d2      500 31 ms
```

- `--endpoint` is the last part of the endpoint's URL. The target can be a port (`3000`), a host and port (`localhost:4000`, `app.test:3000`), or a full URL with any hostname and a path (`https://app.webhookhq.test:3000/webhooks`). See [Custom domains and HTTPS](#custom-domains-and-https).
- Anything after the endpoint URL is added to your app's URL: a webhook sent to `…/acme-stripe-test/webhooks?live=1` arrives at `http://localhost:3000/webhooks?live=1`, with the sender's method, headers and body.
- WebhookHQ answers the sender straight away (with the endpoint's response, or `200`), so senders never wait on your laptop. What your app answered is shown here and in the event's deliveries in the dashboard.
- Webhooks arrive in order while `whq listen` runs. If the connection drops, it reconnects by itself; webhooks that arrive while nothing is listening are captured as usual, and you can replay them.
- `--name` labels this machine in the dashboard (default: its hostname).
- Plans allow a number of tunnels open at once: Free 1, Developer 1, Pro 3, Team 10, Scale 25.

### Custom domains and HTTPS

If your app runs on a local domain (`app.test`, `shop.localhost`, or your own like `app.acme.test`), often over HTTPS, give `whq` the URL you open in the browser:

```sh
whq listen https://app.acme.test:3000/webhooks --endpoint acme-stripe-test
```

- Webhooks go to that host, so your app's host checks and routing (Rails `config.hosts`, virtual hosts in Caddy or nginx, Laravel Valet, Herd) see the name they expect. The path you give comes first, then the webhook's own path and query: `…/acme-stripe-test/stripe?live=1` arrives at `https://app.acme.test:3000/webhooks/stripe?live=1`.
- The name must resolve on your machine (`/etc/hosts`, dnsmasq, or your tool's own resolver), as it does for your browser.
- HTTPS certificates are checked against the ones your computer trusts, the same as your browser. A [mkcert](https://github.com/FiloSottile/mkcert) certificate works once `mkcert -install` has run; Caddy, Valet and Herd certificates work once their certificate authority is trusted. If `whq` says your app's certificate isn't trusted and you're sure it's your own, add `--insecure`. It only affects webhooks sent to your app, never `whq`'s connection to WebhookHQ.
- `whq` doesn't follow redirects, since a redirect would turn the webhook's `POST` into a `GET`. If your app redirects `http://` to `https://`, `whq` shows the URL to use instead.
- `whq replay` takes the same targets and `--insecure`: `whq replay 3f9a2c1 https://app.acme.test:3000/webhooks`.

## Replay a webhook

```sh
whq replay 3f9a2c1 3000
```

Sends a captured webhook (its short id from the dashboard or `whq listen`, or the full id) to your app again, as many times as you like. On every plan. You can also click **Send to my tunnel** on an event while `whq listen` is running.

## Settings

| Variable | |
|---|---|
| `WHQ_TOKEN` | Access token, instead of `whq login` |
| `WHQ_API_URL` | WebhookHQ's address (default `https://app.webhookhq.dev`) |
| `WHQ_CONFIG` | Config file (default `~/.config/whq/config.json`) |
| `NO_COLOR` | Plain output |

## Install

**macOS and Linux, with Homebrew:**

```sh
brew install webhook-hq/tap/whq
whq --version
```

`brew upgrade whq` updates it.

**Without Homebrew** (macOS, Linux, Windows): download the binary for your platform from the [latest release](https://github.com/webhook-hq/whq-cli/releases/latest), check it against `SHA256SUMS`, make it executable and put it on your `PATH`:

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
chmod +x whq-darwin-arm64 && sudo mv whq-darwin-arm64 /usr/local/bin/whq
```

On a Mac, a file downloaded in the browser is quarantined, and the first run says Apple can't check it for malicious software. Clear the flag once with `xattr -d com.apple.quarantine whq-darwin-arm64` before moving it, or install with Homebrew, which doesn't need this.

## Development

```sh
script/build                 # ./whq for this machine
script/build --release       # every platform into dist/
go vet ./... && go test ./...
```
