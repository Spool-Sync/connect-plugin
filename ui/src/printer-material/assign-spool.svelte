<!--
  Puts a spool on a printer tool: SpoolSync's spool picker over every live
  spool, and the footer. The spool the tool holds, if any, is chosen to begin
  with, so Replace is one press on Load. + Create spool makes a spool in the
  dialog it opens and chooses it once it is made.

  The host draws the title and the dialog around it. It shuts the dialog on
  `onclose`, and reads the tools again on `onassigned`.
-->
<script lang="ts">
  import PlusIcon from '@lucide/svelte/icons/plus';
  import { Button, DialogFooter, ListingSkeleton } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import type { MaterialsService, Spool, Tool } from '../api';
  import CreateSpoolDialog from '../create-spool/create-spool-dialog.svelte';
  import { heldHost } from '../host';
  import FaultNote from '../materials-page/fault-note.svelte';
  import SpoolPicker from '../spool-picker/spool-picker.svelte';
  import { refusalText } from './reasons';

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

  let spools = $state.raw<readonly Spool[]>();
  let fault = $state.raw<unknown>(undefined);
  let spoolId = $state<string | undefined>(undefined);
  let creating = $state(false);
  let loading = $state(false);
  let refused = $state.raw<unknown>(undefined);
  let disposed = false;

  /** Reads every live spool, and keeps the choice when the spool is still there. */
  const read = async (choose?: string): Promise<void> => {
    fault = undefined;
    try {
      const live = await service.allSpools();
      if (disposed) return;
      spools = live;
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
      selected={spoolId}
      onselect={(id: string | undefined) => (spoolId = id)}
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

<CreateSpoolDialog
  open={creating}
  onclose={() => (creating = false)}
  oncreated={created}
/>
