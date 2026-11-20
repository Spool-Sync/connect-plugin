# Changelog

What an operator sees change when they upgrade this module, newest release
first. `version:` in `module.yaml` is the newest heading below.

## 0.1.0 - unreleased

### Added

- Every spool a Prusa printer read off its OpenPrintTag NFC tag has its weight
  set in SpoolSync, on the spool with the same instance UUID. The weight sent
  is the filament left in Materials plus the empty spool weight of the SpoolSync
  filament type, and it is sent once per whole gram. All spools are read again
  at start, when the event stream loses continuity, and every 15 minutes.
- The settings `spoolsync_url` and `spoolsync_token` name the SpoolSync server
  and its API token. `materials_module` names the Materials module to follow.
- `GET /status` answers whether the module is configured, and how many spools
  it pushed, how many SpoolSync does not hold, and the last failure.
