# whq CLI

`whq` is WebhookHQ's command-line tool: it delivers an endpoint's webhooks to an app on the developer's machine and replays captured webhooks into it. Customer docs: `README.md`. The server side (the API, `Tunnel`, `TunnelDelivery`, `TunnelChannel`, `EventProcessor`) lives in the WebhookHQ app repository (private); its `docs/CLI.md` must stay in step with `README.md` here.

- Go, module `webhookhq.dev/whq`, one dependency (`github.com/coder/websocket`). Keep it that way: standard library first.
- `script/build` builds `./whq`; `script/build --release` builds every shipped platform into `dist/` with checksums. CI (`.github/workflows/ci.yml`) runs gofmt, vet, tests and the release build on every pull request. Pushing a `v*` tag publishes a GitHub release (`release.yml`) and updates the Homebrew formula in the public `webhook-hq/homebrew-tap` (`Formula/whq.rb`, written by `script/homebrew_formula`; needs the `HOMEBREW_TAP_TOKEN` secret). The repository is public, MIT licensed: never commit anything secret or internal.
- It authenticates only with access tokens (`whq login`, or `WHQ_TOKEN`), saved in `~/.config/whq/config.json` readable only by the user. Never print a token.
- Queued mode: `whq listen` opens a tunnel (`POST /api/v1/tunnels`, all plans, up to the plan's `tunnels` limit) and subscribes to `TunnelChannel` over Action Cable with the token (`Authorization: Bearer`; the CLI sends the app URL as `Origin`). The app creates a delivery per open tunnel and broadcasts the event after commit; the CLI posts it to the local app and reports the result (`result` action). No retries: replay instead (`whq replay <short id>`, or "Send to my tunnel"). Heartbeats every 30 s; the app closes tunnels quiet for 2 minutes.
- Compatibility: released versions keep working against the API, so API changes are additive; a change needing a new CLI says so in the app's `docs/CLI.md`.
- Live mode, the offline queue and path routing (v1.5) need the Go gateway that holds requests open.
- Commit messages: one line, Conventional Commits, lowercase, at most 72 characters.
