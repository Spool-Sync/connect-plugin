# Changelog

What an operator sees change when they upgrade this module, newest release
first. `version:` in `module.yaml` is the newest heading below.

## Unreleased

### Added

- **Filament per print**: the module counts the grams each print takes off
  each spool, from the moment it starts following a print. The Jobs listing
  shows a Filament column, and a print's page lists the spools it used, tool
  by tool. This needs a Prusa Connect with the job slots
  (prusa3d/prusa-connect#80); until a release has them, `go.mod` builds
  against that pull request's SDK.

## 0.2.0 - 2026-11-23

### Added

- **Load spool knows the tool**: the dialog lists only spools of the material
  the printer reports on the tool (PLA also takes WOOD, PC takes PCCF, FLEX
  takes TPU and TPE), without asking for the material, and Create spool starts
  on that material. The spools nearest the colour the printer reports come
  first. A spool already loaded on another tool comes last, greyed out, and
  picking it asks first, naming the printer and tool it is on.
- **Two-way tools**: a spool assigned to a printer's slot, cleared, or a weight
  corrected in SpoolSync's own UI now reaches the module. SpoolSync sends it
  over an authenticated WebSocket feed, and the module adopts it on its tool.
  The later of the two changes wins, so assigning in either UI is not undone
  by the other. The feed needs `printers:view` on the SpoolSync token; a
  SpoolSync without the feed is followed by polling the same snapshot.

### Fixed

- The Tools tab showed a spool's full weight short by the empty spool, so a
  full spool read as more filament left than it held (868 / 668 g).

## 0.1.0 - 2026-11-23

The first release: SpoolSync in Prusa Connect, as a full replacement for the
Materials extension.

### Added

- **The SpoolSync page** (`/spoolsync`): your inventory at a glance (spools in
  stock, kilograms, materials, the filament used over 30 days, the share of
  each material, a tile per material and the filament left of each type),
  above a searchable list of the materials in stock.
- **Spools and materials**: add spools of a catalogue or custom material, edit,
  archive, merge and delete them, keep them in storage locations, star
  favourite materials. The OpenPrintTag catalogue is imported for picking a
  material.
- **The printer's Tools tab**: what is on each tool and MMU slot. Assign a
  spool, create one on the spot, change or unassign it. A spool the printer
  read off an OpenPrintTag NFC tag is added in one step, asking for its weight
  only when neither the tag nor the catalogue knows it, and linked to its
  catalogue material.
- **Weights kept in step**: while a printer prints, the filament it feeds is
  counted off the spool on each tool, from the tag's consumed weight or from
  the tool's odometer, and written once per whole gram.
- **Two ways to run**: with a SpoolSync server (`spoolsync_url` and
  `spoolsync_token`), where SpoolSync keeps the inventory and also sees each
  printer, its job and the spool in each slot; or **standalone**, with both
  left empty, where the module keeps the inventory in its own database and
  imports the catalogue itself.
