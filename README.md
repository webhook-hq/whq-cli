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

- `--endpoint` is the last part of the endpoint's URL. The target can be a port (`3000`), `localhost:4000`, or a URL (`http://127.0.0.1:8080/api`).
- Anything after the endpoint URL is added to your app's URL: a webhook sent to `…/acme-stripe-test/webhooks?live=1` arrives at `http://localhost:3000/webhooks?live=1`, with the sender's method, headers and body.
- WebhookHQ answers the sender straight away (with the endpoint's response, or `200`), so senders never wait on your laptop. What your app answered is shown here and in the event's deliveries in the dashboard.
- Webhooks arrive in order while `whq listen` runs. If the connection drops, it reconnects by itself; webhooks that arrive while nothing is listening are captured as usual, and you can replay them.
- `--name` labels this machine in the dashboard (default: its hostname).
- Plans allow a number of tunnels open at once: Free 1, Developer 1, Pro 3, Team 10, Scale 25.

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

Download the binary for your platform from the [latest release](https://github.com/webhook-hq/whq-cli/releases/latest), check it against `SHA256SUMS`, make it executable and put it on your `PATH`:

```sh
chmod +x whq-darwin-arm64 && mv whq-darwin-arm64 /usr/local/bin/whq
```

## Development

```sh
script/build                 # ./whq for this machine
script/build --release       # every platform into dist/
go vet ./... && go test ./...
```
