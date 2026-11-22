<!--
  One tool or MMU slot on a printer's Tools tab: its position, what is loaded,
  the spool's grams left, and a button that opens the picker in a dialog. It is
  the local equivalent of the library's FilamentPosition component, which the
  published @prusa/connect-ui 0.1.0-beta.1 does not export. The picker is the
  same one the rest of the module uses, handed through `panel`.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  import {
    Button,
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
  } from '@prusa/connect-ui/primitives';

  import { heldHost } from '../host';
  import { OWN_MODULE } from '../own-module';

  let {
    toolId,
    position,
    reported,
    assignedName,
    note,
    assignedColor,
    remainingGrams,
    disabledReason,
    panel,
  }: {
    deviceId: string;
    toolId: string;
    position: string;
    bare?: boolean;
    reported: string | undefined;
    assignedName: string;
    note: string | undefined;
    assignedColor: string | undefined;
    remainingGrams: number | undefined;
    disabledReason: string | undefined;
    panel: Snippet<
      [close: () => void, setTitle: (title?: string) => void, setBusy: (busy: boolean) => void]
    >;
  } = $props();

  const t = heldHost().t;

  let open = $state(false);
  let title = $state<string | undefined>(undefined);
  let busy = $state(false);
  const close = () => (open = false);
</script>

<div
  class="flex flex-col gap-2 rounded-card border p-3"
  data-part="filament-position"
  data-tool={toolId}
>
  <span class="text-xs text-muted-foreground">{position}</span>
  <span class="flex items-center gap-2 font-medium">
    {#if assignedColor}
      <span
        class="inline-block size-3 shrink-0 rounded-full border"
        style:background-color={assignedColor}
        aria-hidden="true"
      ></span>
    {/if}
    {assignedName}
  </span>
  {#if note}
    <span class="text-sm text-muted-foreground">{note}</span>
  {/if}
  {#if remainingGrams !== undefined}
    <span class="text-sm tabular-nums">{t('materials.tools.left', { g: Math.round(remainingGrams) })}</span>
  {/if}
  {#if reported === undefined && disabledReason}
    <p class="text-sm text-muted-foreground">{disabledReason}</p>
  {/if}
  <div>
    <Button
      variant="secondary"
      size="sm"
      disabled={disabledReason !== undefined}
      title={disabledReason}
      pending={busy}
      onclick={() => (open = true)}
    >
      {remainingGrams === undefined && reported === undefined
        ? t('materials.printer.assign')
        : t('materials.printer.change')}
    </Button>
  </div>
</div>

<Dialog bind:open>
  <DialogContent data-module={OWN_MODULE} size="lg">
    <DialogHeader>
      <DialogTitle>{title ?? position}</DialogTitle>
    </DialogHeader>
    {@render panel(close, (next) => (title = next), (next) => (busy = next))}
  </DialogContent>
</Dialog>
