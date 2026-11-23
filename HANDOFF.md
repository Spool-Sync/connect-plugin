# Handoff — 2026-11-20

State of the SpoolSync module for Prusa Connect, for whoever picks it up next.

## Where things are

| what | where |
| --- | --- |
| this repo | `gitlab.honex.life/spoolsync/spool-sync-prusa-connect`, local `/home/domi/Projects/spoolsync/spool-sync-prusa-connect` |
| `main` | the one-way sync that reads the **Materials** module. Every push deploys to connect.honex.life |
| `feat/replace-materials` | the rewrite that drops Materials (pushed? **no**, local only, see below) |
| SpoolSync side | uncommitted worktree `/home/domi/Projects/spoolsync/spoolsync-opt-uuid`, branch `feat/opt-instance-uuid` |
| Prusa Connect checkout used for reference | `/home/domi/orca/workspaces/prusa-connect/auk` (`extensions/materials`, `sdk/`, docs) |
| CDN | `cdn.honex.life`, namespace `spool-sync` |
| deployment | connect.honex.life = k8s context `prague-k3s`, ns `prusa-connect`, deploy `prusa-connect` |

## What works on main

- One-way sync: follows Materials' `<ns>.spool` events plus a full re-read at
  start and every 15 min; finds the SpoolSync spool by OpenPrintTag
  `instance_uuid` (`GET /api/v1/spools/by-opt/:uuid`) and writes
  `remaining + spoolWeight_g` (SpoolSync weights are gross) with
  `PUT /spools/:id/update-weight`, `source: "prusa-connect"`.
- `scripts/release.sh` builds the release `.tar.gz` Connect installs (flat:
  `module.yaml`, `openapi.json`, `<name>_<GOOS>_<GOARCH>` binaries,
  `checksums.txt`). Verified with Connect's real `module add`.
- CI (`.gitlab-ci.yml`):
  - every MR/push: `go vet`, `go test -race`, build;
  - **main** → `deploy:connect`: uploads to the CDN as
    `spool-sync-prusa-connect-dev.tar.gz`, then `POST /api/v1/modules` on
    connect.honex.life with `from=<cdn url>?build=<iid>`, `sha256`,
    `approve: true`;
  - **v\* tag** → `publish`: CDN file `spool-sync-prusa-connect-<version>.tar.gz`
    and KV `value-store/spool-sync/spool-sync-prusa-connect`. Tags never deploy.
- CI variables (project, masked, unprotected): `HONEX_CDN_TOKEN` (CDN key
  "spool-sync-prusa-connect CI", namespace `spool-sync`),
  `CONNECT_ADMIN_TOKEN`.

## Lessons paid for (do not repeat)

1. **Versions.** Connect's manifest accepts `1.2.0-beta2`, but the handshake
   (`extension.Version`) takes **MAJOR.MINOR.PATCH only**, and the binary must
   answer exactly the manifest's version. So: `main.go` has `var version`,
   stamped by `-ldflags -X main.version=…`; dev builds are
   `MAJOR.MINOR.(100000 + pipeline iid)` via `DEV_BUILD=<n>`. A test keeps
   `main.go`'s default equal to `module.yaml`'s version.
2. Cloudflare caches `cdn.honex.life/file/…` by full URL; the `?build=<iid>`
   query gets past a stale copy (the CDN ignores it).
3. Connect downloads only from public https: use `https://spoolsync.honex.life`,
   never `spoolsync.prague.k3s`. Egress defaults to port 443.
4. glab: `-h` is `--help`, not host. Use `GITLAB_HOST=gitlab.honex.life`.
5. connect.honex.life has **no record store** (`resources.db` is not
   declared), so `stateapi` calls fail there. Use `needs.database` (moduledb).

## Open right now

- **Last main deploy** (pipeline 2322, commit `ff5b95e`, first build numbered
  `0.1.100xxx`) was still running at handoff. Check:
  `GITLAB_HOST=gitlab.honex.life glab ci list -R spoolsync/spool-sync-prusa-connect`
  and `kubectl -n prusa-connect logs deploy/prusa-connect | grep spool-sync`.
  Connect still runs `0.1.0-dev.4`, which fails to start (version mismatch);
  a good deploy replaces it.
- **Materials is not installed on connect.honex.life**, so main's sync has
  nothing to read. That is why the rewrite below exists.
- **SpoolSync API token:** the Settings → API Tokens dialog creates tokens with
  **no permissions** (it sends only a name), so they 403 everywhere. Create one
  from the browser console on spoolsync.honex.life:
  ```js
  fetch('/api/v1/api-tokens', { method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + localStorage.getItem('token') },
    body: JSON.stringify({ name: 'Prusa Connect', permissions: ['spools:view','spools:edit','spools:create','filament_types:view'] }),
  }).then(r => r.json()).then(t => console.log(t.token))
  ```
  Then set `spoolsync_url` / `spoolsync_token` in Connect → Settings →
  Extensions → SpoolSync. Settings survive redeploys.
- **SpoolSync `feat/opt-instance-uuid` is uncommitted and undeployed.** Without
  it there is no `by-opt` lookup and no spool is ever matched. It adds
  `Spool.optInstanceUuid` (+ migration `20261120000001_…`, backfill derived from
  the chip UID per the OpenPrintTag spec), `GET /spools/by-opt/:uuid`, and a
  `source` on `update-weight`. Its main checkout has someone else's
  uncommitted changes; leave them alone.

## Next: replace Materials (`feat/replace-materials`)

The full plan, with the route contract and JSON shapes, is
**`docs/replace-materials.md`** on that branch. Committed so far
(`f3a8d37`, local, not pushed): the plan and the new `module.yaml`. No code
yet. In short:

- follow printers directly (`deviceapi` + `busapi`, as
  `extensions/materials/internal/printers/follower.go` does);
- tagged tool: weight = `actual_netto_full_weight − consumed_weight` from the
  printer's telemetry, spool by `material_package_instance/uuid`;
- untagged tool: a person assigns a SpoolSync spool in the printer's Spools
  tab; count down from `odometer_e`/`odometer_weight` (copy Materials'
  `internal/consume`, OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1);
- own SQLite DB (assignments, last odometer readings, pending grams);
- UI bundle (Svelte 5, `@prusa/connect-ui@0.1.0-beta.1` from npm): Spools
  page, spool page, Catalogue (SpoolSync filament types), printer Spools tab;
- token needs `spools:view`, `spools:edit`, `spools:create`,
  `filament_types:view`;
- release.sh and CI also build `ui/` and ship `ui/dist` as `ui/` in the archive.

The plan was to split this between two agents: one for the Go side (follower,
consume, db, routes, openapi.json), one for `ui/` plus release/CI. Push the
branch only when it builds and passes; merging to main deploys it.

## Ideas raised, not started

- SpoolSync: add permission checkboxes to the Create Token dialog.
- Spool events from this module (like Materials' `<ns>.spool` / `<ns>.tool`)
  for other modules: low-filament warnings, a "will it fit" check before a
  print, Home Assistant sensors, cost per print.
- SpoolSync's web OPT ingest pre-fills `currentWeight_g` as filament only while
  elsewhere it is gross; the first push corrects it by ~the empty spool.
- A printer SpoolSync also polls over PrusaLink gets weight from both sides;
  last write wins.
