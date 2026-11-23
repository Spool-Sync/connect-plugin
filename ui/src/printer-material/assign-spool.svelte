<!--
  Puts a spool on a printer tool: SpoolSync's spool picker over every live
  spool of the material the printer reports on the tool (every spool when it
  reports none, or a name of its own), and the footer. The spool the tool holds, if any, is chosen to begin
  with, so Replace is one press on Load. + Create spool makes a spool in the
  dialog it opens and chooses it once it is made.

  The host draws the title and the dialog around it. It shuts the dialog on
  `onclose`, and reads the tools again on `onassigned`.
-->
<script lang="ts">
  import PlusIcon from '@lucide/svelte/icons/plus';
  import {
    Button,
    ConfirmDialog,
    DialogFooter,
    ListingSkeleton,
  } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import type { MaterialsService, Spool, Tool } from '../api';
  import CreateSpoolDialog from '../create-spool/create-spool-dialog.svelte';
  import { heldHost } from '../host';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { requiredMaterial } from './material-match';
  import SpoolPicker from '../spool-picker/spool-picker.svelte';
  import { refusalText } from './reasons';
  import { toolLabel } from './tool-label';

  let {
    service,
    deviceId,
    tool,
    onclose,
    onassigned,
    onbusy,
  }: {
    service: MaterialsService;
    deviceId: string;
    tool: Tool;
    /** Cancel. */
    onclose: () => void;
    /** The tool took the spool; the host reads its tools again. */
    onassigned: () => void;
    /** A load is in flight, so the host should not be shut under it. */
    onbusy?: (busy: boolean) => void;
  } = $props();

  const t = heldHost().t;

  /** The material the printer holds on the tool, which the spool must be. */
  const material = $derived(requiredMaterial(tool.reported?.abbreviation));
  /** The colour the printer reports on the tool; the spools nearest it come first. */
  const colour = $derived(tool.reported?.color_rgba || undefined);

  let spools = $state.raw<readonly Spool[]>();
  let fault = $state.raw<unknown>(undefined);
  let spoolId = $state<string | undefined>(undefined);
  let creating = $state(false);
  let loading = $state(false);
  let refused = $state.raw<unknown>(undefined);
  /** A spool loaded on another tool, picked and waiting for the person to confirm. */
  let asking = $state.raw<Spool | undefined>(undefined);
  /** The printers the spools are loaded on, by id, for naming them. */
  let printerNames = $state.raw<ReadonlyMap<string, string>>(new Map());
  let disposed = false;

  const here = $derived({ device_id: deviceId, tool_id: tool.id });

  /** Whether the spool is loaded on a tool other than this one. */
  const elsewhere = (s: Spool): boolean =>
    s.tool != null && (s.tool.device_id !== deviceId || s.tool.tool_id !== tool.id);

  /** Names the printers the live spools are on; a printer it cannot read keeps its id. */
  const readPrinters = async (live: readonly Spool[]): Promise<void> => {
    const ids = [...new Set(live.flatMap((s) => (s.tool == null ? [] : [s.tool.device_id])))];
    if (ids.length === 0) return;
    try {
      const printers = await service.printers(ids);
      if (!disposed) printerNames = new Map(printers.map((p) => [p.id, p.name]));
    } catch {
      // The question still names the tool; the printer's id stands in.
    }
  };

  /** A spool on another tool is chosen only once the person confirms it. */
  const select = (id: string | undefined): void => {
    const picked = spools?.find((s) => s.id === id);
    if (picked !== undefined && elsewhere(picked)) {
      asking = picked;
      return;
    }
    spoolId = id;
  };

  /** Reads every live spool, and keeps the choice when the spool is still there. */
  const read = async (choose?: string): Promise<void> => {
    fault = undefined;
    try {
      const live = await service.allSpools();
      if (disposed) return;
      spools = live;
      void readPrinters(live);
      const wanted = choose ?? tool.assignment?.spool_id;
      spoolId = live.some((spool) => spool.id === wanted) ? wanted : undefined;
    } catch (thrown) {
      if (!disposed) fault = thrown;
    }
  };

  onMount(() => {
    void read();
    return () => {
      disposed = true;
    };
  });

  const load = async (): Promise<void> => {
    const picked = spoolId;
    if (picked === undefined || loading) return;
    loading = true;
    onbusy?.(true);
    refused = undefined;
    try {
      await service.assign(deviceId, tool.id, { spool_id: picked });
      onassigned();
    } catch (thrown) {
      refused = thrown;
    } finally {
      loading = false;
      onbusy?.(false);
    }
  };

  const created = (id: string): void => {
    creating = false;
    void read(id);
  };
</script>

<div class="flex min-w-0 flex-col gap-4">
  {#if spools === undefined && fault === undefined}
    <ListingSkeleton label={t('materials.printer.readingSpools')} rows={3} />
  {:else if spools !== undefined}
    <SpoolPicker
      {spools}
      {material}
      {colour}
      {here}
      selected={spoolId}
      onselect={select}
    />
  {/if}

  <FaultNote {fault} />
  {#if refused !== undefined}
    {@const text = refusalText(
      t,
      refused,
      'assign',
      tool.reported?.abbreviation ?? '',
    )}
    {#if text}
      <p data-part="refusal" class="text-sm text-destructive">{text}</p>
    {:else}
      <FaultNote fault={refused} />
    {/if}
  {/if}

  <DialogFooter>
    <Button
      type="button"
      variant="secondary"
      class="mr-auto"
      onclick={() => (creating = true)}
    >
      <PlusIcon aria-hidden="true" />
      {t('materials.assign.create')}
    </Button>
    <Button type="button" variant="secondary" onclick={onclose}>
      {t('materials.add.cancel')}
    </Button>
    <Button
      type="button"
      disabled={spoolId === undefined || loading}
      onclick={() => void load()}
    >
      {t('materials.assign.load')}
    </Button>
  </DialogFooter>
</div>

<ConfirmDialog
  open={asking !== undefined}
  onOpenChange={(open: boolean) => {
    if (!open) asking = undefined;
  }}
  title={t('materials.assign.elsewhereTitle')}
  description={asking?.tool == null
    ? ''
    : t('materials.assign.elsewhere', {
        tool: toolLabel(t, asking.tool.tool_id),
        printer: printerNames.get(asking.tool.device_id) ?? asking.tool.device_id,
      })}
  confirmLabel={t('materials.assign.elsewhereConfirm')}
  variant="confirm"
  onConfirm={() => {
    spoolId = asking?.id;
    asking = undefined;
  }}
/>

<CreateSpoolDialog
  open={creating}
  type={material}
  onclose={() => (creating = false)}
  oncreated={created}
/>
