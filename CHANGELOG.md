# Changelog

What an operator sees change when they upgrade this module, newest release
first. `version:` in `module.yaml` is the newest heading below.

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
