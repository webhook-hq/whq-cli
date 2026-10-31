# CLI and tunnels

The server side of tunnels lives in the app (`Tunnel`, `TunnelDelivery`, `TunnelChannel`, `EventProcessor`); these rules cover both.

- `cli/` is the `whq` CLI: Go (module `webhookhq.dev/whq`, one dependency, `github.com/coder/websocket`), documented for customers in `docs/CLI.md`. `script/build_cli` builds `tmp/whq` (`--release` for every platform); CI runs gofmt, vet and tests. It authenticates only with access tokens.
- Queued mode: `whq listen` opens a `Tunnel` (`POST /api/v1/tunnels`, all plans, up to the plan's `tunnels` limit) and subscribes to `TunnelChannel` over Action Cable with the token (the connection identifies `access_token` from `Authorization: Bearer`; the CLI sends the app URL as `Origin`). `EventProcessor` creates a `Delivery` per open tunnel (`tunnel_id` instead of `destination_id`, `TunnelDelivery`) and broadcasts the event after commit; the CLI posts it to the local app and reports the result (`result` action). No retries: replay instead (`whq replay <short id>`, or "Send to my tunnel"). Heartbeats every 30 s; `TunnelCleanupJob` closes tunnels quiet for 2 minutes and fails what they hadn't answered.
- Live mode, the offline queue and path routing (v1.5) need the Go gateway that holds requests open.
