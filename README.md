# SpoolSync for Prusa Connect

> Maintained on [GitLab](https://gitlab.honex.life/spoolsync/spool-sync-prusa-connect).
> [GitHub](https://github.com/Spool-Sync/connect-plugin) is a read-only mirror;
> see [CONTRIBUTING](CONTRIBUTING.md).

A Prusa Connect module that replaces the Materials extension. SpoolSync is the
only inventory: materials are SpoolSync filament types, spools are SpoolSync
spools, locations are SpoolSync storage locations. The module follows every
printer directly, works out which spool is on each tool, and keeps its weight
in SpoolSync in step with what the printer reports.

It also runs **standalone**, with no SpoolSync server: leave `spoolsync_url`
and `spoolsync_token` empty and the module keeps the inventory in its own
database (see [Standalone](#standalone)).

It serves Materials' route set with the same paths, query parameters, bodies and
refusal rules, and emits Materials' `tool` and `spool` events under its own
namespace. The browser half (`ui/`) draws the same pages.

## What it does

- **Follows the printers** through the host (`observe`): each tool's material,
  its NFC tag, its odometer and its consumed weight.
- **Works out the spool on each tool.** Tagged: the spool with the tag's
  instance uuid, filament left = full weight − consumed weight. Assigned: a spool
  a person put on the tool, counted down from the odometer by density and
  diameter. A tool may also carry a material with no spool. A tag on a tool
  clears its assignment.
- **Writes SpoolSync at most once per whole gram**, as the gross weight: filament
  left plus the empty spool. Grams counted from an odometer wait in the module's
  database until a whole gram is due, and at the end of a job.
- **Assigns and unassigns** through the Materials rules: a printing printer
  refuses a change, a tool that reads a tag refuses an assignment, an empty tool
  refuses one, an offline printer accepts. A person must be allowed to `operate`
  the printer (`access.check`, kept 30 seconds).
- **Archives** spools (leave the stock totals, cannot go on a tool) and keeps
  favourites and list filters per person in its own database.
- **Catalogue** is SpoolSync's OpenPrintTag import, started from the module.

## Settings

| key | type | default | what it controls |
| --- | --- | --- | --- |
| `spoolsync_url` | url | | The SpoolSync server, without `/api/v1`. Empty with the token: standalone. |
| `spoolsync_token` | secret | | The API token sent as `Authorization: Bearer`. Set both or neither. |
| `max_spools` | int | `2000` | How many spools the module lets the inventory hold before it refuses to add another. |

The module needs a database (`needs.database`): SQLite in the module's data
directory, or Postgres if the operator picks it at install. It holds the
assignments, the last odometer reading of each tool, the grams not yet written,
favourites and list filters.

## Standalone

With no SpoolSync server set, the spools, filament types, storage locations and
each spool's weight readings live in the module's database (tables
`filament_types`, `locations`, `spools` and `weight_history`). The routes, the
pages and the printer follower work as they do on SpoolSync; the weight rules
are SpoolSync's (a weight is gross, the filament left is the gross less the
empty spool).

- **Catalogue:** the module downloads the OpenPrintTag database itself
  (`internal/catalogue`, the same reading as SpoolSync's importer) and imports
  it on its first start, and again from the Catalogue settings.
- **Usage chart:** drawn from the readings the module records, so it starts
  empty.
- **Left out:** the SpoolSync app (its web UI, ESP32 scales and NFC weigh
  station) and the Prusa Connect Local printer mirror.
- Moving an inventory between standalone and SpoolSync is not built: changing
  the settings starts the module on the other inventory, and the first one is
  kept as it was.

## Token permissions

The SpoolSync token needs every one of these:

`spools:view`, `spools:create`, `spools:edit`, `spools:delete`,
`filament_types:view`, `filament_types:create`, `filament_types:edit`,
`filament_types:delete`, `storage:view`, `storage:create`, `storage:edit`,
`storage:delete`.

Creating a token with fewer makes the matching routes answer
`spool-sync/SPOOLSYNC_REFUSED`.

## Routes

Materials' route set, mounted at `/api/v1/modules/<namespace>/`. `openapi.json`
describes each one, its permission and its refusals. `GET /status` reports what
the follower has done: the stats, the printers followed and the tools holding a
spool. Errors are problem+json with a `spool-sync/` code; `faults.go` lists them,
and a test holds them to the `Refusal` enum of `openapi.json`.

Two differences from Materials are on purpose: spool and location ids are
SpoolSync's strings, not integers, and a material's uuid is its SpoolSync
filament type id.

## Events

Declared under `events:` in `module.yaml`, with Materials' payloads:

| type | key | payload |
| --- | --- | --- |
| `<namespace>.tool` | `<device_id>/<tool_id>` | `device_id`, `tool_id`, `material_uuid`, `spool_id` |
| `<namespace>.spool` | `spool/<id>` | `spool_id`, `spool_uuid`, `material_uuid`, `remaining_weight_g`, `deleted` |

A spool is told once per whole gram it loses while printing. An extension that
read Materials must point at this namespace.

## Develop

```sh
go test -race ./...
SPOOLSYNC_TEST_POSTGRES_DSN='postgres://user:pass@localhost/postgres?sslmode=disable' go test ./...
```

The store tests run on SQLite always and on Postgres when the variable names a
server; the test creates and drops its own database on it.

It builds against `github.com/prusa3d/prusa-connect/sdk` at the version in
`go.mod`.

## Install

From the Prusa Plugin Marketplace, as `com.github.aux.spool-sync.connect-plugin` (module
path `github.com/spool-sync/connect-plugin`), once a release is approved there.

## Releasing to the marketplace

A tag `v<version>` (the version in `module.yaml`, whose heading in
`CHANGELOG.md` gives the release notes) pushed on GitLab is mirrored to
GitHub, where `.github/workflows/release.yml` hands the commit to the
marketplace's builder. It builds every platform and the bundle
(`scripts/marketplace-bundle.sh`), signs the release with the key in the
GitHub environment `release` (secret `PRUSA_CONNECT_EXTENSION_SIGNING_KEY`)
and uploads it for review. `.github/workflows/marketplace-check.yml` runs the
same build as a dry run on every push to main.

## Release and install

`sh scripts/release.sh` builds `dist/spool-sync-prusa-connect-<version>.tar.gz`,
the archive Prusa Connect installs, and its sha256 beside it.
`DEV_BUILD=42` builds `MAJOR.MINOR.100042` instead, for a build that is no
release. Connect's handshake takes `MAJOR.MINOR.PATCH` only, so a dev build
cannot carry a `-dev` suffix.

CI tests and builds every MR and push. Then:

- **main** is installed on `connect.honex.life` (job `deploy:connect`), as
  `MAJOR.MINOR.(100000 + pipeline number)`, such as `0.1.100006`. The archive goes through the Honex CDN as
  `spool-sync-prusa-connect-dev.tar.gz`, because Connect downloads only over
  public https. Installing over the installed build is the update: settings
  and the SpoolSync token are kept.
- **a `v<version>` tag** is published for installs elsewhere, and deploys
  nothing. A tag that does not match `module.yaml` fails the build.

  | what | where |
  | --- | --- |
  | the archive | `https://cdn.honex.life/file/spool-sync/spool-sync-prusa-connect-<version>.tar.gz` |
  | the latest release | `https://cdn.honex.life/value-store/spool-sync/spool-sync-prusa-connect`, as `{module, version, url, sha256, commit, published_at}` |

CI needs two masked variables: `HONEX_CDN_TOKEN`, a CDN key scoped to the
`spool-sync` namespace, and `CONNECT_ADMIN_TOKEN`, a Connect token with the
admin role. A protected variable reaches only protected refs, so protect `v*`
tags if `HONEX_CDN_TOKEN` is protected.

Install or update another server from the latest published release:

```sh
curl -fsS https://cdn.honex.life/value-store/spool-sync/spool-sync-prusa-connect |
  jq '{module, version, from: .url, sha256, approve: true}' |
  curl -fsS -X POST https://connect.example.net/api/v1/modules \
    -H "Authorization: Bearer $CONNECT_ADMIN_TOKEN" \
    -H "Content-Type: application/json" --data-binary @-
```

## Licence

OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1, the licence of Prusa Connect, in
[LICENSE](LICENSE). Built on Prusa Connect by Prusa Research a.s.: the module
derives from Prusa Connect's Materials extension (its tool rules, its
consumption math and much of its UI) and is built on the Prusa Connect SDK and
UI library, so it is distributed under the same licence and add-ons. In short:
free to use, change and share for individuals and small businesses; a change
you share stays under this licence and keeps the credit to Prusa Research; a
business over EUR 1M a year needs a licence from Prusa, and no business may run
it as a hosted service for others. The LICENSE file holds the full text.

The OpenPrintTag database the module downloads is MIT licensed and is not
shipped with it.

## Known limits

- SpoolSync has no owner on a custom material or a spool, so custom materials
  are shared, where Materials kept them per person. Favourites and list filters
  are per person.
- A material has no packages and no temperatures: SpoolSync holds none.
- Merging does not move a spool's weight history.
- `POST /spools/delete` checks every spool first, but SpoolSync has no
  transaction, so a failure part way leaves the earlier deletes done.
- A printer that SpoolSync also polls over PrusaLink gets its weight from both;
  the later write wins.
