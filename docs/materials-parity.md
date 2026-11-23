# Materials parity: the module is a full drop-in for Materials

Added 2026-11-21. This widens `docs/replace-materials.md`: where the two
disagree, this file wins. The reference for every behaviour is the Materials
extension in `/home/domi/orca/workspaces/prusa-connect/auk/extensions/materials`
(its `README.md` describes each rule; its `openapi.json`, `internal/routes`,
`internal/assign`, `internal/printers` and `ui/src` implement them). Match its
behaviour feature by feature. The data lives in SpoolSync instead of
Materials' own tables, except where noted.

## Data mapping

| Materials | here |
| --- | --- |
| material (catalogue row, OpenPrintTag material_uuid) | SpoolSync filament type; `opt_material_uuid` holds the OpenPrintTag material uuid (SpoolSync field `optMaterialUuid`, being added) |
| catalogue download / import | SpoolSync `POST /filament-types/sync-openprinttag` (being added): imports the OpenPrintTag database into filament types. The module's Catalogue settings tab triggers it and shows its progress/result |
| custom material | SpoolSync filament type with `custom: true` (`POST/PUT/DELETE /filament-types`) |
| favourites, saved list filters | the module's own database, per Connect person (they are about the person in Connect, not the inventory) |
| spool | SpoolSync spool |
| spool source `printer` / `manual` | `printer` when it has an `optInstanceUuid`, else `manual` |
| spool archive (archived_at, reason, note) | SpoolSync `archivedAt`, `archiveReason`, `archiveNote` (being added); archived spools leave stock totals and location counts and cannot go on a tool |
| spool notes | SpoolSync `notes` |
| location (max 100) | SpoolSync storage location (`/storage-locations`) |
| tool assignment (material or spool) | the module's own database, as planned; a tool may carry a material without a spool |
| odometer state | the module's own database, as planned |
| merge a manual spool into a printer spool | the module: copy location/notes where the printer spool has none, move weight history if SpoolSync allows, delete the manual spool |
| `max_spools` setting | module option `max_spools` (int, default 2000); refuse adding past it |

## Routes: the same paths and shapes as Materials

Implement Materials' route set, with the same paths, query parameters,
bodies, answers and refusal rules, so its UI logic and any caller port over:

```
GET  /catalogue                 GET  /catalogue/brands         POST /catalogue/download
GET  /types                     GET  /materials                GET  /materials/{uuid}
GET  /materials/{uuid}/spools   POST /custom-materials         PATCH/DELETE /custom-materials/{uuid}
PUT/DELETE /favourites/{uuid}   GET/POST /locations            PATCH/DELETE /locations/{id}
GET/PUT /list-filters           GET/POST /spools               PATCH/DELETE /spools/{id}
POST /spools/delete             POST /spools/{id}/merge
GET  /printers                  GET  /printers/tools           GET  /printers/{device}/tools
PUT/DELETE /printers/{device}/tools/{tool}                     POST /printers/{device}/tools/{tool}/spool
GET  /status
```

`{uuid}` for a material is the filament type's id (use the OpenPrintTag
material uuid when it has one, else the SpoolSync id; be consistent and say
which in openapi.json). Where Materials' JSON has a field SpoolSync cannot
fill, leave it out or null and list it in your report. This replaces the
smaller route table in `replace-materials.md`.

Every refusal rule in Materials' README "Materials on printers" applies:
printing/paused refuse a change, busy refuses a change to a tool that reports
its material, untrusted states refuse, a loaded material must match, an empty
tool refuses an assignment, an NFC-read tool refuses an assignment but allows
unassign, offline accepts. `GET /printers/{device}/tools` says why a change
would be refused. The person must be allowed to `operate` the printer
(`access.check`, cached 30 s per person/action/printer). Reads take
`view_resource`, writes `operate_resource` (the host does that; state it in
openapi.json as Materials does). Error codes start with `spool-sync/`.

Unload (on a connected printer's active head, and an MMU's head) sends the
printer the same command the Controls tab sends. Look at how Materials'
UI does it (it is a UI action through the host's device API, not a module
route); copy that.

## Events: the same payloads, under this module's namespace

Declare and emit `tool` and `spool` events as Materials does (manifest
`events:`; types `<namespace>.tool` / `<namespace>.spool`, keys
`<device_id>/<tool_id>` and `spool/<id>`, same payload fields, coalesced, once
per whole gram while printing). Another module that follows Materials must be
pointed at this namespace; that is expected.

## UI: everything Materials draws

Pages (`materials` list, `material` hidden detail), settings tabs (`catalogue`,
`locations`), device tab (`tools`), the add dialog (1 to 100 spools at once,
custom material form), archive in the edit dialog, merge, colour swatches,
location field, NFC mark, "Add to inventory" for a tag, live updates from the
module's own `tool`/`spool` events, and the `printer-positions` export.
Keep Materials' page paths and slot ids so links and muscle memory carry
over. Port Materials' tests that cover the same logic.

Known limit to report, not to solve: the Core's printer page
(`frontend/src/lib/screens/printer-detail.svelte`) draws the
`printer-positions` export of the Materials namespace only, by name. The
export is shipped anyway; the Core needs a change to look it up by export
name instead.

## Token

The SpoolSync token needs: `spools:view`, `spools:create`, `spools:edit`,
`spools:delete`, `filament_types:view`, `filament_types:create`,
`filament_types:edit`, `filament_types:delete`, `storage:view`,
`storage:create`, `storage:edit`, `storage:delete`, `printers:view`, `printers:edit` (the printers are reported to SpoolSync: see `mirror.go`).

## Where the module differs from Materials (review, 2026-11-22)

What the Go side does differently, mostly because SpoolSync holds less:

1. Ids are strings (spool, location, `spool_id`, `ids[]`, `into`), not integers.
2. A material's `{uuid}` is the SpoolSync filament type id; `opt_material_uuid`
   is carried and used for NFC matching, but there is no lookup by it.
3. Material detail: `packages` is always `[]`; no temperatures, `color_family`
   or `capacity_g`; class is FFF; density and diameter only when SpoolSync has them.
4. A spool's `created_by` is always empty: SpoolSync records no creator.
5. Custom materials are shared, not per person: SpoolSync types have no owner.
   Favourites and list filters stay per person.
6. Brands: `uuid` and `slug` are the brand name; `GET /types` is derived from
   the filament types.
7. `GET /printers` adds `total` and `next`; `GET /printers/tools` pages by
   printer and its `total` counts tool rows.
8. A tool's `assign`/`unassign` reasons use the tool's own reported material,
   not the one a person would pick.
9. Catalogue: the import runs in SpoolSync (`sync-openprinttag`); `loaded` is
   "some type has an OpenPrintTag uuid"; no `catalogue_url`/`catalogue_ca`.
10. Extra codes: `spool-sync/SPOOLSYNC_UNAVAILABLE` (502, retryable),
    `spool-sync/SPOOLSYNC_REFUSED` (502), `spool-sync/NOT_CONFIGURED` (503);
    no `BUSY` (store errors answer `STORE_FAILED`).
11. Adding N spools, deleting several and merging are N SpoolSync calls, not
    one transaction. Merge does not move weight history.
12. Weights are written as whole grams of gross weight; a fraction waits in
    the database. Events are one per write, not coalesced on a timer.

Checked against a real SpoolSync (local, the full OpenPrintTag catalogue):
every client call in `internal/spoolsync` passes `live_test.go`.

Found on the SpoolSync side: `GET /filament-types` ignores paging and answers
every type (16,507 after a catalogue import); the module searches instead.
