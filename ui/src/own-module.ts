// The namespace the host serves this bundle under, read from the bundle's own
// address, as the library's style injection reads it. The module's routes, its
// events and its dialogs' `data-module` all name it.
//
// Served from anywhere else (a test, a dev page) the address names nothing, so
// the namespace the module is declared under stands in. That is the one the
// install address derives: `life.honex.gitlab.aux.spoolsync.spool-sync-prusa-connect`.
const DECLARED = 'life.honex.gitlab.aux.spoolsync.spool-sync-prusa-connect';

const served = decodeURIComponent(
  /\/modules\/([^/]+)\/ui\//u.exec(import.meta.url)?.[1] ?? '',
);

export const OWN_MODULE = served === '' ? DECLARED : served;
