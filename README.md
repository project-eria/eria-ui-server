# ui-user

Dashboard to control the ERIA services (lights, shutters, modes…). Design and plan: [PLAN.md](PLAN.md).
Widget packs (add widgets without recompiling): [PACKS.md](PACKS.md). Accessibility: [REPORT.md](REPORT.md).

- `backend/` — Go: mDNS discovery, `/api/*`, WebSocket multiplexer, serves the built UI.
- `frontend/` — SvelteKit shell + Lit widgets.

## Develop

```sh
cd frontend && pnpm install && pnpm dev   # proxies /api to the test bench (ERIA_BACKEND to change)
pnpm check && pnpm test
cd ../backend && go test ./...
```

Local backend for browser tests: `config.yml` with `database: dev.db`, and test PINs in
`backend/dev.env` (git-ignored): `set -a; . ./dev.env; set +a; ./ui-user --config-path config.yml`.

## Build

```sh
cd frontend && pnpm build                 # writes ../backend/web (app + /sdk/eria-widgets.js)
cd ../backend && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ui-user .
UI_USER_ADMIN_PIN=<4-8 digits> ./ui-user --config-path config.yml   # first start creates profile 'Admin'
```

The Go module uses the published `eria-core` / `go-wot`: build with `GOWORK=off`.

## Release and deployment (ERIA orchestrator)

This folder is the `project-eria/ui-user` repository. Pushing a `vX.Y.Z` tag runs
`.github/workflows/release.yml`: frontend and backend tests, then GoReleaser
(`.goreleaser.yaml`) publishes `ui-user_linux_arm64`, `ui-user_linux_amd64`,
`eria-app.yml` and `checksums.txt`. The master lists the version in its catalog;
deploy it from ui-admin (Catalogue tab):

- **pin it to one node**: profiles, sessions and screens live in the agent's data
  folder of that node (`{data}/ui-user.db`, kept across reinstalls);
- in the config, set `adminPin` (first start only, then change the PIN in *Profiles*).

Dry run without publishing: `goreleaser release --snapshot --clean` (in a git checkout).
