# Replacing Materials

Branch `feat/replace-materials`. Main auto-deploys to connect.honex.life, so
nothing lands there until this is done and reviewed.

## Goal

The module stops depending on the Materials extension. SpoolSync is the only
inventory. The module:

1. follows every printer directly (deviceapi + busapi telemetry), as
   `extensions/materials/internal/printers/follower.go` in the prusa-connect
   checkout does (`/home/domi/orca/workspaces/prusa-connect/auk`);
2. works out each tool's spool and its filament left, and writes the gross
   weight to SpoolSync (`PUT /spools/{id}/update-weight`, source
   `prusa-connect`), at most once per whole gram;
3. serves routes its own UI reads, proxying SpoolSync with the module's token;
4. ships a UI bundle: a Spools page, a spool page, a Catalogue page (SpoolSync
   filament types), and a printer Tools tab.

## Which spool is on a tool

Per printer tool (head, or feeder slot on a head with an MMU):

- **Tagged.** The tool reports `material_package_instance/uuid` (the
  OpenPrintTag `instance_uuid`). The spool is the SpoolSync spool from
  `GET /spools/by-opt/{uuid}`. Filament left =
  `actual_netto_full_weight - consumed_weight` when both are reported.
- **Assigned.** No tag uuid. A person picked a SpoolSync spool for this tool
  in the Tools tab. The module stores `(printer_id, tool) -> spoolsync spool
  id` in its own database. Filament left is counted down from the odometer.
- **None.** Neither: nothing is written.

A tagged reading always wins over an assignment on the same tool, and a tag
appearing on a tool clears that tool's assignment.

### Odometer counting (assigned, or tagged without consumed_weight)

Keys on the tool: `odometer_id`, `odometer_e` (mm since reset),
`odometer_weight` (g, optional). Keep the last `(odometer_id, odometer_e,
odometer_weight)` per tool in the database. On each reading:

- different or first `odometer_id` -> reset, credit nothing;
- same id, lower `odometer_e` -> stale, ignore;
- otherwise grams = delta of `odometer_weight` if the printer reports it, else
  `delta_mm * pi/4 * diameter_mm^2 * density / 1000` with the SpoolSync
  filament type's `diameter_mm` (default 1.75) and `density_g_cm3` (default by
  material type, table in `extensions/materials/internal/consume`, else 1.24).

Subtract the grams from SpoolSync's current gross weight and write it. Batch:
write when the accumulated grams for a spool reach >= 1 g, or at job end.
Copy the small consume math (MIT, prusa-connect repo) into
`internal/consume`, with its tests.

## Storage

`needs: database: true` in module.yaml (SQLite in the module's data dir by
default; the operator may choose Postgres). Use `sdk/moduledb` as
`extensions/materials` does, with migrations. Tables:

- `assignments(printer_id, tool, spool_id, assigned_at, assigned_by)`, PK `(printer_id, tool)`
- `odometers(printer_id, tool, odometer_id, e_mm, weight_g, updated_at)`, PK `(printer_id, tool)`
- `pending(spool_id, grams)`: grams counted and not yet written

## Routes (module's own, mounted at `/api/v1/modules/<namespace>/`)

All JSON. Errors are problem+json with a code, as `faults.go` does today.
Writes are made on behalf of the person: ask `accessapi` whether they may
`operate` the printer before an assignment change (manifest `checks:
[operate]`, as Materials does). Spool edits only need the host's own
`view_resource`.

| route | does |
| --- | --- |
| `GET /status` | as now, plus `printers` followed and `tools` with a spool |
| `GET /spools?search=&status=&page=&limit=` | SpoolSync `GET /spools`, mapped to `Spool` |
| `GET /spools/{id}` | one `Spool`, plus `history` from SpoolSync `GET /spools/{id}/history` |
| `POST /spools` | create in SpoolSync (`filament_type_id`, `initial_weight_g`, optional `location`, `opt_instance_uuid`) |
| `PUT /spools/{id}` | edit in SpoolSync (name fields, location, status) |
| `PUT /spools/{id}/weight` | `{ "remaining_g": n }` sets the weight by hand (gross = n + empty spool) |
| `GET /filament-types?search=` | SpoolSync filament types, mapped to `FilamentType` |
| `GET /printers/{printer_id}/tools` | `[]Tool` for one printer |
| `PUT /printers/{printer_id}/tools/{tool}` | `{ "spool_id": "..." }` assigns |
| `DELETE /printers/{printer_id}/tools/{tool}` | unassigns |

`tool` is the tool path as text: `0`, `1`, … for heads, `0.3` for slot 3 of
the feeder on head 0.

```jsonc
// Spool
{
  "id": "spoolsync spool id",
  "name": "Prusament PLA Galaxy Black",   // brand + material + colour, as SpoolSync shows it
  "brand": "Prusament", "material": "PLA", "color": "#3d3e3dff",
  "remaining_g": 812.7,                    // gross - empty spool, never below 0
  "gross_g": 1012.7, "empty_g": 200, "initial_g": 1200,
  "status": "IN_USE",                      // SpoolSync status, as is
  "opt_instance_uuid": "…" | null,         // null = untagged
  "location": "Shelf A" | null,
  "on_tool": { "printer_id": "…", "tool": "0" } | null,
  "updated_at": "RFC 3339"
}
// FilamentType
{ "id": "…", "brand": "…", "material": "…", "name": "…", "color": "#rrggbb",
  "diameter_mm": 1.75, "density_g_cm3": 1.24, "spool_weight_g": 200, "net_weight_g": 1000 }
// Tool
{ "tool": "0", "label": "Head 1" /* or "Slot 4" */,
  "source": "tag" | "assigned" | "none",
  "tag": { "instance_uuid": "…", "type": "PLA", "color": "#…", "full_g": 1000, "consumed_g": 187.3 } | null,
  "spool": Spool | null,                   // null when source is none, or the tag is not in SpoolSync
  "unknown_tag": true | false }            // a tag SpoolSync holds no spool for
```

The SpoolSync side is in `/home/domi/Projects/spoolsync/spoolsync-opt-uuid`
(Express + Prisma; routes in `backend/src/routes`, the spool shape in
`backend/prisma/schema.prisma` and `backend/src/services/spoolService.js`).
Read it for the field names; do not change it.

## Token

The SpoolSync token needs `spools:view`, `spools:edit`, `spools:create` and
`filament_types:view`. Update the option summary and the README.

## Manifest

Done in `module.yaml` on this branch: `materials_module`, `suggest`, `calls`
and `core-routes.call` are gone; `require` lists `core-device.list`,
`core-device.get`, `core-bus.subscribe`, `core-access.check`; `needs.database`;
`checks: [operate]`; the `ui` block with pages `spools`, `spool` (hidden),
`catalogue`, and the `tools` device tab. Do not change it without saying so.

## UI

Svelte 5 + `@prusa/connect-ui` from npm (`0.1.0-beta.1`), in `ui/`, built to
`ui/dist` and shipped in the release under `ui/`. Model it on
`extensions/materials/ui` (how it registers with `host.register`, reads its
own routes, uses the component library and locales), but it is much smaller:

- **Spools page:** a searchable table of spools: colour swatch, name,
  remaining g with a bar of remaining/initial, status, location, on which
  printer tool. Add spool (pick a filament type, initial weight, location).
- **Spool page:** details, set weight by hand, edit, history list.
- **Catalogue page:** filament types, searchable; Add spool from a row.
- **Tools tab** (device_tab, handed the printer id): one row per tool:
  tagged spool (read-only, "from tag"), assigned spool with Change/Unassign,
  or an Assign picker of untagged spools. An unknown tag says "Tag not in
  SpoolSync" with a button that adds it (POST /spools with
  `opt_instance_uuid`).

Every text in `ui/src/locales/en.json`. Tests with vitest where Materials has
them for the same thing.

## Release and CI

`scripts/release.sh` (which already takes `TAG=` and `DEV_BUILD=`) builds the bundle (`pnpm install --frozen-lockfile &&
pnpm build` in `ui/`) and copies `ui/dist` into the release as `ui/`. CI
`build` uses an image with Node 22 and pnpm 11, and runs the UI tests in
`test`.
