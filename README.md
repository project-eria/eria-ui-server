# eria-ui-server

Relay between the ERIA app ([eria-ui-app](https://github.com/project-eria/eria-ui-app)) and
the services of the house (lights, shutters, modes…): mDNS discovery of the WoT Things,
`/api/*`, one WebSocket multiplexing the observations, profiles + PIN, custom screens, widget
packs (`packs/<id>/`, contract in eria-ui-app's `PACKS.md`). Design and plan: eria-ui-app's `PLAN.md` (§2.1).

## Develop

```sh
GOWORK=off go test ./...
```

Local server: `config.yml` (from `config.sample.yml`) with `database: dev.db`, and test PINs
in `dev.env` (git-ignored): `set -a; . ./dev.env; set +a; go run . --config-path config.yml`.

## Build

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build -o eria-ui-server .
ERIA_UI_SERVER_ADMIN_PIN=<4-8 digits> ./eria-ui-server --config-path config.yml   # first start creates profile 'Admin'
```

The module uses the published `eria-core` / `go-wot`: build with `GOWORK=off`.

## Release and deployment (ERIA orchestrator)

Repository `project-eria/eria-ui-server`. Pushing a `vX.Y.Z` tag runs the shared ERIA release
workflow (`project-eria/workflows`): GoReleaser publishes the raw binaries, `eria-app.yml`
and `checksums.txt`, listed by the master's catalog. Deploy it from the Catalogue:

- **pin it to one node**: profiles, sessions and screens live in the agent's data folder of
  that node (`{data}/eria-ui-server.db`, kept across reinstalls);
- in the config, set `adminPin` (first start only, then change the PIN in *Profiles*).
