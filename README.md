# eria-ui-server

Relay between the ERIA app ([eria-ui-app](https://github.com/project-eria/eria-ui-app)) and
the services of the house (lights, shutters, contexts…): mDNS discovery of the WoT Things, one
WebSocket per app for commands, observations, events, settings and the tunnel to auth (below).
It keeps no state (AUTH-DESIGN §3.6): tokens are issued by **eria-auth**, checked here with the
auth keyset signed by the master and auth's signed revocation list; pages, start pages,
contexts, hidden and manual Things are auth's, read every 3 s. Design: eria-ui-app's `PLAN.md`
(§2.1, §2.2) and `AUTH-DESIGN.md`.

## Channel (`wss://…/api/ws`)

TLS with the relay's role certificate (`relais-interieur` or `relais-exterieur`), signed by
the master. Handshake header `Authorization: Bearer <token>` (a terminal or person token from
auth); a refused token gets 401. Without a token, only the tunnel ops and `token` are open:
the app enrolls and gets its first token through the tunnel, then sends it. The connection is
closed 30 s after its token's `exp` unless the app sent a new one, and as soon as the revocation
list makes its token fall. The Rust client sends no `Origin`; a cross-origin page is refused.

The app sends `{"id", "op", …}`; each request gets one reply `{"id", "result"}` or
`{"id", "error": {"type", "message"}}`, with `type` one of `offline`, `refused`, `timeout`,
`invalid`, `failed`. Replies may arrive in any order.

| Op | Fields | Result |
|---|---|---|
| `tunnelOpen` | — | `{tunnel}`: a connection to auth (at most 4); its bytes come back pushed |
| `tunnelData` | `tunnel`, `value`: base64 bytes | `true` |
| `tunnelClose` | `tunnel` | `true` |
| `token` | `value`: token | `true`; the first one, then each renewal (same terminal only) |
| `reportType` | `value`: `watch` … `tv` | `true` (screen deduced by the app, passed to auth) |
| `services` | — | services, each with `can {read, write, invoke}` for this connection |
| `contexts` | — | list |
| `page` | `value`: id | page |
| `pages` | — | every page |
| `savePage` | `value`: page (no `id` = new, with a known `theme` — `""` = Base; same `version` as stored; `theme`, `aspect`, `layout` fixed after creation) | page, saved by auth (a signed-in person of level ≤ 1) |
| `deletePage` | `value`: id | `true` (same) |
| `setHomePage` | `value`: page id, `""` = none | `true`; the house's start page (same) |
| `read` | `thing`, `name` | value |
| `write` | `thing`, `name`, `value` | `null` (level < 9) |
| `invoke` | `thing`, `name`, `value` (input) | output (level < 9) |
| `notices` | — | notices of service-notices this connection may see, newest first, each with `read` (`audience: profile` ones for a signed-in person only; a resolved one leaves the list 24 h after its resolution once read — read or not without a person); `[]` without the service |
| `markRead` | `value`: notice ids | `true` (a signed-in person) |
| `observe` / `unobserve` | `thing`, `name` (property) | `true` |
| `subscribe` / `unsubscribe` | `thing`, `name` (event) | `true` |
| `themes` | — | signed themes of the themes folder `{id, name, version, hash, aspects, layouts, report, error?}`, for creating a page (level ≤ 1) |
| `theme` | `value`: id | `{archive (base64), signature}` |
| `things` | — | every known Thing `{id, title, url, online, hidden, manual}` (level ≤ 1) |
| `td` | `thing` | the Thing's TD as received (level ≤ 1) |

Pushed: `welcome` (without a token), or `welcome {terminal, level, signedIn}` then
`resources {pages: [{id, version}], themes: [{id, hash}], homePage}` (every page, and the
signed themes they use; again whenever the configuration changes), then
`value {thing, property, value}`, `event {thing, event, value}`, `online {thing, online}`,
`things-changed`, `contexts-changed`, `notices-changed` (ask `notices` again),
`tunnel {tunnel, data}` (base64) and `tunnel-closed {tunnel}`.

The administration (profiles, terminals, start pages, contexts, hidden and manual Things) is
done by eria-ui-admin with auth directly, never through the relay. Hidden Things are offered
to no app. `can` is provisional (reading for all, commands below level 9) until `authz`
computes it.

Besides the channel, HTTP only serves the health probe, in plain HTTP on the node only
(`health:`, default `127.0.0.1:8099`, `/api/health`).

## Develop

```sh
GOWORK=$PWD/../golang/go.work go test ./...
```

Until go-wot's `auth` package is published, the relay builds within the workspace of
`../golang/go.work`. Local relay: `config.yml` (from `config.sample.yml`), its TLS files from
eria-manager (`eria-ui-server csr`, `tls-sign`, `tls-root`, `auth-keyset`) and a running
eria-auth.

## Build

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o eria-ui-server .
```

## Release and deployment (ERIA orchestrator)

Repository `project-eria/eria-ui-server`. Pushing a `vX.Y.Z` tag runs the shared ERIA release
workflow (`project-eria/workflows`): GoReleaser publishes the raw binaries, `eria-app.yml`
and `checksums.txt`, listed by the master's catalog. Deploy it from the Catalogue, then put
its TLS files in its data folder (`eria-app.yml`). It keeps no state: it can move freely.
