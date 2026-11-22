<!--
  Says that a spool added by hand and a spool a printer read off its NFC tag
  are one spool. The person picks the printer spool, and the dialog names
  both before the merge. The printer spool stays, and the other leaves the
  list.

  The page lists the spools a page at a time, so the dialog reads the pages
  the page has not read yet. It offers every printer spool of the material.
-->
<script lang="ts">
  import {
    Button,
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    Label,
    ListingSkeleton,
    RadioGroup,
    RadioGroupItem,
  } from '@prusa/connect-ui/primitives';
  import { formatNumber } from '@prusa/connect-ui/i18n';
  import { untrack } from 'svelte';

  import type { MaterialsService, Spool } from '../api';
  import { heldHost } from '../host';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { OWN_MODULE } from '../own-module';

  let {
    service,
    spool,
    loaded,
    next,
    busy,
    fault,
    onmerge,
    onclose,
  }: {
    service: MaterialsService;
    /** The spool added by hand; the dialog is open while there is one. */
    spool: Spool | undefined;
    /** The spools of the material the page has read. */
    loaded: readonly Spool[];
    /** Where the page's next unread page starts, or undefined on the last. */
    next: number | undefined;
    busy: boolean;
    fault: unknown;
    onmerge: (into: string) => void;
    onclose: () => void;
  } = $props();

  const t = heldHost().t;
  const uid = $props.id();

  let into = $state<string | undefined>(undefined);
  /** The printer spools of the material, undefined while they are read. */
  let targets = $state.raw<readonly Spool[] | undefined>(undefined);
  let readFault = $state.raw<unknown>(undefined);

  // Each opening reads the rest of the pages and starts on the first printer
  // spool. Closing the dialog stops the read.
  $effect(() => {
    const opened = spool;
    if (opened === undefined) return;
    const stop = new AbortController();
    untrack(() => {
      targets = undefined;
      into = undefined;
      readFault = undefined;
      const all = [...loaded];
      const read = async (): Promise<void> => {
        let offset = next;
        while (offset !== undefined) {
          const page = await service.spools(
            opened.material_uuid,
            offset,
            stop.signal,
          );
          all.push(...page.spools);
          offset = page.next ?? undefined;
        }
      };
      read().then(
        () => {
          targets = all.filter(
            (s) => s.source === 'printer' && s.archived_at == null,
          );
          into = targets[0]?.id;
        },
        (thrown: unknown) => {
          if (!stop.signal.aborted) readFault = thrown;
        },
      );
    });

    return () => {
      stop.abort();
    };
  });
</script>

<Dialog
  open={spool !== undefined}
  onOpenChange={(next: boolean) => {
    if (!next) onclose();
  }}
>
  <!-- The panel is portalled, so it carries the module's name for its
       utilities. -->
  <DialogContent data-module={OWN_MODULE}>
    <DialogHeader>
      <DialogTitle>
        {t('materials.merge.title', { id: spool?.id ?? 0 })}
      </DialogTitle>
    </DialogHeader>
    <div class="flex flex-col gap-2">
      <span id="{uid}-pick" class="text-sm font-medium">
        {t('materials.merge.pick')}
      </span>
      {#if targets === undefined}
        {#if readFault === undefined}
          <ListingSkeleton label={t('materials.merge.reading')} rows={2} />
        {/if}
      {:else if targets.length === 0}
        <p class="text-sm text-muted-foreground">
          {t('materials.merge.none')}
        </p>
      {/if}
      <RadioGroup
        value={into === undefined ? '' : String(into)}
        aria-labelledby="{uid}-pick"
        onValueChange={(next: string) => {
          into = next;
        }}
      >
        {#each targets ?? [] as target (target.id)}
          <div class="flex items-center gap-2">
            <RadioGroupItem value={String(target.id)} id="{uid}-{target.id}" />
            <Label for="{uid}-{target.id}">
              {t('materials.merge.target', {
                id: target.id,
                g: formatNumber(target.remaining_weight_g, {
                  maximumFractionDigits: 0,
                }),
              })}
            </Label>
          </div>
        {/each}
      </RadioGroup>
    </div>
    {#if spool !== undefined && into !== undefined}
      <DialogDescription>
        {t('materials.merge.ask', { from: spool.id, into })}
      </DialogDescription>
    {/if}
    <FaultNote fault={readFault ?? fault} />
    <DialogFooter>
      <Button type="button" variant="secondary" onclick={onclose}>
        {t('materials.add.cancel')}
      </Button>
      <Button
        type="button"
        disabled={into === undefined || busy}
        onclick={() => {
          if (into !== undefined) onmerge(into);
        }}
      >
        {t('materials.merge.confirm')}
      </Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
