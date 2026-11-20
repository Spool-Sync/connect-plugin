# SpoolSync for Prusa Connect

A Prusa Connect module that keeps [SpoolSync](https://spool-sync.com/) in step
with what Prusa printers report about their NFC-tagged spools.

It follows the Materials module. When a printer reads an OpenPrintTag spool,
Materials holds that spool under the tag's `instance_uuid` and counts its
filament down as it prints. This module sets the weight of the SpoolSync spool
with the same `instance_uuid`.

It is one way: nothing is written back to Prusa Connect.

## What it needs

- **Materials** installed in Prusa Connect. The module calls it and reads its
  spool events; the operator approves both at install.
- **SpoolSync** with the `optInstanceUuid` field and the
  `GET /api/v1/spools/by-opt/:uuid` route (branch `feat/opt-instance-uuid`).
  SpoolSync fills the field for every spool it holds a chip UID for, so
  existing spools need no re-scan.
- A SpoolSync **API token** (`ss_...`) with `spools:view` and `spools:edit`.

## Settings

| key | type | what it does |
| --- | --- | --- |
| `spoolsync_url` | url, required | The SpoolSync server, such as `https://spools.example.net`, without `/api/v1`. |
| `spoolsync_token` | secret, required | The API token sent as `Authorization: Bearer`. |
| `materials_module` | string | The Materials namespace to follow. Default `com.github.aux.prusa3d.materials`. |

## How a weight is worked out

Materials counts the filament alone. SpoolSync's `currentWeight_g` is what a
scale reads: filament and empty spool. So the module sends
`remaining_weight_g + filamentType.spoolWeight_g`, rounded to 0.1 g, and only
when it differs from SpoolSync's by half a gram or more. The weight history in
SpoolSync records these readings with the source `prusa-connect`.

A spool added by hand in Materials has a UUID of Materials' own, which no tag
carries, so it is skipped. A tagged spool SpoolSync does not hold is counted as
`unknown` in `GET /status` and asked about again at the next full read.

## Routes

| route | what it answers |
| --- | --- |
| `GET /status` | `configured`, and `stats`: `pushed`, `unknown`, `failed`, `last_push`, `last_error`, `last_full_sync` |

## Develop

```sh
go test -race ./...
```

It builds against `github.com/prusa3d/prusa-connect/sdk` at the version in
`go.mod`. To build against a local checkout instead:

```sh
go mod edit -replace github.com/prusa3d/prusa-connect/sdk=../prusa-connect/sdk
```

## Release and install

`sh scripts/release.sh` builds `dist/spool-sync-prusa-connect-<version>.tar.gz`,
the archive Prusa Connect installs, and its sha256 beside it. CI does the same
on every push, and a `v<version>` tag also publishes it to the Honex CDN:

| what | where |
| --- | --- |
| the archive | `https://cdn.honex.life/file/spool-sync/spool-sync-prusa-connect-<version>.tar.gz` |
| the latest release | `https://cdn.honex.life/value-store/spool-sync/spool-sync-prusa-connect`, as `{module, version, url, sha256, commit, published_at}` |

To release, set `version` in `module.yaml`, add it to `CHANGELOG.md`, and push
the tag `v<version>`. A tag that does not match `module.yaml` fails the build.
Publishing needs `HONEX_CDN_TOKEN`, a masked and protected CI variable holding
a CDN key scoped to the `spool-sync` namespace, so `v*` tags are protected.

Install or update a server from the latest release:

```sh
curl -fsS https://cdn.honex.life/value-store/spool-sync/spool-sync-prusa-connect |
  jq '{module, version, from: .url, sha256, approve: true}' |
  curl -fsS -X POST https://connect.example.net/api/v1/modules \
    -H "Authorization: Bearer $CONNECT_ADMIN_TOKEN" \
    -H "Content-Type: application/json" --data-binary @-
```

Installing over an installed release is the update. The settings and the
token are kept.

## Known limits

- One way only.
- A printer that SpoolSync also polls (PrusaLink) gets its weight from both:
  SpoolSync subtracts at the end of a job, and this module sets the absolute
  weight Materials counted. The later write wins.
