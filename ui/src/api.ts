// The service layer: the module's own routes, reached through the host's
// client (svelte-data.md, frontend-structure.md).
//
// `bind` comes from `./client`, the typed client generated from
// `client/openapi.json`. The client the host hands over is in session mode, so
// the credential is the cookie the browser already holds, and a refusal is
// thrown as the problem document the module sent, with its `code`.
import type { ModuleHost } from '@prusa/connect-ui/slots';
import { bind, type components, type operations } from './client';

/**
 * A detail as a list row, for the picker that reads a material it was handed
 * from a row. The detail's fields are all optional in the contract; a field
 * it lacks reads as the empty value the row takes.
 */
export const rowOf = (detail: Detail): Row => ({
  uuid: detail.uuid ?? '',
  name: detail.name ?? '',
  brand_name: detail.brand_name ?? '',
  class: detail.class ?? 'FFF',
  type: detail.type ?? '',
  color_rgba: detail.color_rgba ?? '',
  is_favourite: detail.is_favourite ?? false,
  is_custom: detail.is_custom ?? false,
  spools: detail.spools ?? 0,
  assigned_spools: detail.assigned_spools ?? 0,
  remaining_weight_g: detail.remaining_weight_g ?? 0,
  free_g: detail.free_g ?? 0,
});

/** One material of the list, wherever it comes from. */
export type Row = components['schemas']['Row'];

/** One page of the list, with the stock numbers that frame it. */
export type Page = components['schemas']['Page'];

/** One catalogue brand: the slug is what the brand filter takes. */
export type Brand = components['schemas']['Brand'];

/** What the database holds and what the importer is doing. */
export type Catalogue = components['schemas']['Catalogue'];

/** One material with what the add dialog shows of it and its packages. */
export type Detail = components['schemas']['Detail'];

/** Every field of a custom material, as the form sends it. */
export type MaterialFields = components['schemas']['MaterialFields'];

/** One spool as the person typed it, and how many of it to add. */
export type SpoolsAdded = components['schemas']['SpoolsAdded'];
export type SpoolsDraft = components['schemas']['SpoolsDraft'];

/** One spool of a material, as the module keeps it. */
export type Spool = components['schemas']['Spool'];
/** The stock at a glance, for the dashboard. */
export type Overview = components['schemas']['Overview'];

/** One page of a material's spools, and how many it has. */
export type SpoolPage = components['schemas']['Spools'];
/** One page of printers, and where the next one starts. */
export type PrinterPage = Awaited<
  ReturnType<ReturnType<typeof bind>['listPrinters']>
>;
/** How many printers one page of the printer picker reads. */
export const PRINTER_PAGE = 50;
/** How many spools one page of the list reads: the most the module answers. */
const SPOOL_PAGE = 100;

/** One place spools are kept, shared by everyone, and its live spools. */
export type Location = components['schemas']['Location'];

/** The fields of a spool to change; a field left out stays. */
export type SpoolPatch = components['schemas']['SpoolPatch'];

/** One printer the person may observe, and whether they may operate it. */
export type Printer = components['schemas']['Printer'];

/** One printer and its tools, with what the caller may do on each. */
export type PrinterTools =
  operations['list-tools']['responses']['200']['content']['application/json'];

/** One tool of a printer, with what is on it and why a change is refused. */
export type Tool = components['schemas']['Tool'];

/** What is on one tool. */
export type Assignment = components['schemas']['Assignment'];

/** Why a change to a tool is refused, empty when it is allowed. */
export type Reason = components['schemas']['Reason'];

/** A material or a spool to put on a tool, one of the two. */
export type AssignRequest = components['schemas']['AssignRequest'];

/** The weights a tag without one needs: what is left, and what the spool held new. */
export interface ReportedWeights {
  readonly remaining_weight_g: number;
  readonly full_weight_g?: number;
}

/** What the person narrowed the list to. A field left out keeps every row. */
export type ListQuery = NonNullable<
  operations['list-materials']['parameters']['query']
>;

/** What the stores need of the module's routes. A test hands in a fake. */
export interface MaterialsService {
  /** The stock at a glance: totals, by material, by type, and the usage chart. */
  overview(signal?: AbortSignal): Promise<Overview>;
  list(query: ListQuery, signal?: AbortSignal): Promise<Page>;
  star(uuid: string, on: boolean, signal?: AbortSignal): Promise<void>;
  brands(signal?: AbortSignal): Promise<readonly Brand[]>;
  /** The material types of the catalogue and the caller's custom materials. */
  types(signal?: AbortSignal): Promise<readonly string[]>;
  /** The list filters the caller kept last, or an empty object. */
  filters(signal?: AbortSignal): Promise<Readonly<Record<string, unknown>>>;
  /** Keeps the caller's list filters in place of the last ones. */
  keepFilters(filters: ListQuery, signal?: AbortSignal): Promise<void>;
  /** Every location by name, with its live spools. */
  locations(signal?: AbortSignal): Promise<readonly Location[]>;
  /** Makes a location, or answers the one that has the name in any case. */
  createLocation(name: string, signal?: AbortSignal): Promise<Location>;
  /** Renames a location for every spool kept there. */
  renameLocation(
    id: string,
    name: string,
    signal?: AbortSignal,
  ): Promise<Location>;
  /** Deletes a location; its spools are left with none. */
  deleteLocation(id: string, signal?: AbortSignal): Promise<void>;
  catalogue(signal?: AbortSignal): Promise<Catalogue>;
  download(signal?: AbortSignal): Promise<Catalogue>;
  material(uuid: string, signal?: AbortSignal): Promise<Detail>;
  /** Makes a custom material and answers its uuid. */
  createCustom(fields: MaterialFields, signal?: AbortSignal): Promise<string>;
  /** One page of the live spools of one material, oldest first. */
  spools(
    uuid: string,
    offset?: number,
    signal?: AbortSignal,
  ): Promise<SpoolPage>;
  /**
   * Every live spool SpoolSync holds, from each page of the list in turn. An
   * archived spool is left out: the picker never offers one.
   */
  allSpools(signal?: AbortSignal): Promise<readonly Spool[]>;
  updateSpool(
    id: string,
    patch: SpoolPatch,
    signal?: AbortSignal,
  ): Promise<Spool>;
  deleteSpool(id: string, signal?: AbortSignal): Promise<void>;
  /** Deletes all of them in one transaction, or none. */
  deleteSpools(ids: readonly string[], signal?: AbortSignal): Promise<void>;
  /** A full replace of the custom material's fields. */
  updateCustom(
    uuid: string,
    fields: MaterialFields,
    signal?: AbortSignal,
  ): Promise<void>;
  deleteCustom(uuid: string, signal?: AbortSignal): Promise<void>;
  /** Adds the spools and answers how many were added. */
  addSpools(draft: SpoolsDraft, signal?: AbortSignal): Promise<SpoolsAdded>;
  /** The printers with these ids that the caller may observe, by name. */
  printers(
    ids: readonly string[],
    signal?: AbortSignal,
  ): Promise<readonly Printer[]>;
  /**
   * One page of the printers the caller may observe whose name, model or id
   * holds `q`, starting at `offset`: 0, or the `next` of the page before.
   */
  printerPage(
    q: string,
    offset: number,
    signal?: AbortSignal,
  ): Promise<PrinterPage>;
  /** One printer's tools. A printer the caller may not observe is NOT_FOUND. */
  tools(device: string, signal?: AbortSignal): Promise<PrinterTools>;
  /** Puts a material or a spool on a tool. */
  assign(
    device: string,
    tool: string,
    body: AssignRequest,
    signal?: AbortSignal,
  ): Promise<Assignment>;
  /** Takes the person's assignment off a tool. */
  unassign(device: string, tool: string, signal?: AbortSignal): Promise<void>;
  /**
   * Adds the spool the printer read off its NFC tag, and puts it on the tool.
   * A tag that holds no full weight needs the weights here, or the module
   * refuses with WEIGHT_NEEDED.
   */
  addReported(
    device: string,
    tool: string,
    weights?: ReportedWeights,
    signal?: AbortSignal,
  ): Promise<Spool>;
  /** Folds a spool added by hand into the printer spool `into`. */
  merge(id: string, into: string, signal?: AbortSignal): Promise<Spool>;
}

/** The module's routes, bound onto the client the host handed this module. */
export const materialsOn = (host: ModuleHost): MaterialsService => {
  const routes = bind(host.client);
  // Every call also ends with the host's signal, so nothing this module asked
  // for carries on once the person who admitted it has left.
  const ended = (signal?: AbortSignal): AbortSignal =>
    signal === undefined ? host.signal : AbortSignal.any([signal, host.signal]);

  return {
    overview: async (signal) => await routes.readOverview({}, ended(signal)),
    list: async (query, signal) =>
      await routes.listMaterials({ query }, ended(signal)),
    star: async (uuid, on, signal) => {
      const request = { path: { uuid } };
      if (on) await routes.starMaterial(request, ended(signal));
      else await routes.unstarMaterial(request, ended(signal));
    },
    brands: async (signal) =>
      (await routes.listBrands({}, ended(signal))).brands,
    types: async (signal) => (await routes.listTypes({}, ended(signal))).types,
    filters: async (signal) =>
      (await routes.getListFilters({}, ended(signal))).filters,
    keepFilters: async (filters, signal) => {
      await routes.keepListFilters({ body: filters }, ended(signal));
    },
    locations: async (signal) =>
      (await routes.listLocations({}, ended(signal))).locations,
    createLocation: async (name, signal) =>
      await routes.createLocation({ body: { name } }, ended(signal)),
    renameLocation: async (id, name, signal) =>
      await routes.renameLocation(
        { path: { id }, body: { name } },
        ended(signal),
      ),
    deleteLocation: async (id, signal) => {
      await routes.deleteLocation({ path: { id } }, ended(signal));
    },
    catalogue: async (signal) => await routes.readCatalogue({}, ended(signal)),
    download: async (signal) =>
      await routes.downloadCatalogue({}, ended(signal)),
    material: async (uuid, signal) =>
      await routes.getMaterial({ path: { uuid } }, ended(signal)),
    createCustom: async (fields, signal) =>
      (await routes.createCustomMaterial({ body: fields }, ended(signal))).uuid,
    spools: async (uuid, offset, signal) =>
      await routes.listMaterialSpools(
        { path: { uuid }, query: offset === undefined ? {} : { offset } },
        ended(signal),
      ),
    allSpools: async (signal) => {
      // The list reads at most 100 spools a page, and `next` is the offset of
      // the page after, or none on the last one.
      const live: Spool[] = [];
      let offset: number | undefined;
      do {
        const query = offset === undefined ? {} : { offset };
        const page = await routes.listSpools(
          { query: { ...query, limit: SPOOL_PAGE } },
          ended(signal),
        );
        live.push(...page.spools.filter((spool) => spool.archived_at == null));
        // A page that does not move on would read the same spools again.
        const next = page.next ?? undefined;
        offset = next !== undefined && next > (offset ?? 0) ? next : undefined;
      } while (offset !== undefined);

      return live;
    },
    updateSpool: async (id, patch, signal) =>
      await routes.updateSpool({ path: { id }, body: patch }, ended(signal)),
    deleteSpool: async (id, signal) => {
      await routes.deleteSpool({ path: { id } }, ended(signal));
    },
    updateCustom: async (uuid, fields, signal) => {
      await routes.updateCustomMaterial(
        { path: { uuid }, body: fields },
        ended(signal),
      );
    },
    deleteCustom: async (uuid, signal) => {
      await routes.deleteCustomMaterial({ path: { uuid } }, ended(signal));
    },
    deleteSpools: async (ids, signal) => {
      await routes.deleteSpools({ body: { ids: [...ids] } }, ended(signal));
    },
    addSpools: async (draft, signal) =>
      routes.addSpools({ body: draft }, ended(signal)),
    printers: async (ids, signal) =>
      (await routes.listPrinters({ query: { id: [...ids] } }, ended(signal)))
        .printers,
    printerPage: async (q, offset, signal) =>
      await routes.listPrinters(
        { query: { q, offset, limit: PRINTER_PAGE } },
        ended(signal),
      ),
    tools: async (device, signal) =>
      await routes.listTools({ path: { device } }, ended(signal)),
    assign: async (device, tool, body, signal) =>
      await routes.assignTool({ path: { device, tool }, body }, ended(signal)),
    unassign: async (device, tool, signal) => {
      await routes.unassignTool({ path: { device, tool } }, ended(signal));
    },
    addReported: async (device, tool, weights, signal) =>
      await routes.addReportedSpool(
        weights === undefined
          ? { path: { device, tool } }
          : { path: { device, tool }, body: weights },
        ended(signal),
      ),
    merge: async (id, into, signal) =>
      await routes.mergeSpool({ path: { id }, body: { into } }, ended(signal)),
  };
};
