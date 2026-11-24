// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives.
//
// `defineModule` is an identity function. It exists so an author gets the
// host's types at this call site without importing the host.
import { defineModule } from '@prusa/connect-ui/slots';
import type { Component } from 'svelte';

import { mayOperate } from './access';
import { materialsOn } from './api';
import { Catalogue, holdCatalogue } from './catalogue.svelte';
import { CatalogueSettings } from './catalogue-settings';
import JobCell from './job-filament/job-cell.svelte';
import JobSection from './job-filament/job-section.svelte';
import { LocationsSettings } from './locations-settings';
import { holdHost } from './host';
import { holdList, MaterialList } from './list.svelte';
import { MaterialPage } from './material-page';
import { MaterialsPage } from './materials-page';
import { PrinterMaterial } from './printer-material';
import './styles.css';

export default defineModule((host) => {
  // Everything a component of this module needs is built here, from the host
  // and from nothing else. The host arrives before any component is mounted,
  // so it is held rather than threaded down.
  holdHost(host);
  const service = materialsOn(host);
  // The page is the SpoolSync inventory: what is in stock, never the catalogue.
  // The add and assign pickers keep their own lists, which search it all.
  holdList(
    new MaterialList(service, mayOperate(host), { in_stock: true }),
    host.signal,
  );
  holdCatalogue(new Catalogue(service), host.signal);

  // host.signal fires when the person who signed in leaves. Every request of
  // host.client ends with it. Pass it to a call that takes a signal, and stop
  // every timer and stream you start when it fires, for example:
  //
  //   const timer = setInterval(refresh, 5000);
  //   host.signal.addEventListener('abort', () => clearInterval(timer));
  //   await client.listThings({}, host.signal);

  // Every registration below is held to `contributes` in module.yaml: a name
  // the manifest does not declare is refused at run time and the app drops
  // this module. The generator wrote both from one answer, so they agree; keep
  // them in step by hand after that.
  host.register.page('spoolsync', MaterialsPage);
  host.register.page('materials', MaterialsPage);
  host.register.page('material', MaterialPage);
  host.register.settings('catalogue', CatalogueSettings);
  host.register.settings('locations', LocationsSettings);
  host.register.deviceTab('tools', PrinterMaterial);
  host.register.component('printer-positions', PrinterMaterial);

  // The filament each print took (ADR-20261123-wfw). A Prusa Connect without
  // the job slots offers no way to register them, and the module works on
  // without them.
  const jobs = host.register as Partial<
    Record<
      'jobColumn' | 'jobSection',
      (id: string, component: Component<{ jobId: string }>) => void
    >
  >;
  jobs.jobColumn?.('filament', JobCell);
  jobs.jobSection?.('filament', JobSection);
});
