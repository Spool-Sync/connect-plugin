<!--
  The filament one print took, on the print's page: one row a spool and tool,
  with the spool's colour, its name, the tool it fed and the grams, and the
  total under them. The host draws the section's heading.
-->
<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';
  import { Skeleton } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { materialsOn, type JobUsage } from '../api';
  import { cssColour } from '../css-colour';
  import { heldHost } from '../host';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { toolLabel } from '../printer-material/tool-label';

  let { jobId }: { jobId: string } = $props();

  const host = heldHost();
  const t = host.t;
  const service = materialsOn(host);

  let usage = $state.raw<JobUsage | undefined>(undefined);
  let fault = $state.raw<unknown>(undefined);

  onMount(() => {
    const stop = new AbortController();
    service
      .jobUsage(jobId, stop.signal)
      .then((read) => (usage = read))
      .catch((thrown: unknown) => {
        if (!stop.signal.aborted) fault = thrown;
      });
    return () => {
      stop.abort();
    };
  });

  const grams = (g: number): string =>
    t('materials.printer.grams', {
      g: formatNumber(g, { maximumFractionDigits: g < 10 ? 1 : 0 }),
    });
</script>

<div class="flex flex-col gap-3" data-part="job-filament">
  <FaultNote {fault} />
  {#if usage === undefined && fault === undefined}
    <Skeleton class="h-10 w-full" />
  {:else if usage !== undefined && usage.spools.length === 0}
    <p class="text-sm text-muted-foreground">{t('materials.job.none')}</p>
  {:else if usage !== undefined}
    <ul class="flex flex-col gap-2">
      {#each usage.spools as row (row.tool + '/' + row.spool_id)}
        {@const colour = cssColour(row.color_rgba)}
        <li class="flex items-center gap-3 text-sm" data-part="job-spool">
          <span
            class="size-5 shrink-0 rounded-full border border-border"
            style:background-color={colour}
            aria-hidden="true"
          ></span>
          <span class="flex min-w-0 flex-1 flex-col">
            <span class="truncate font-medium">
              {row.name === ''
                ? t('materials.job.gone')
                : `${row.brand} ${row.name}`}
            </span>
            <span class="text-xs text-muted-foreground">
              {toolLabel(t, row.tool)}{row.material === '' ? '' : ` · ${row.material}`}
            </span>
          </span>
          <span class="shrink-0 tabular-nums">{grams(row.grams)}</span>
        </li>
      {/each}
    </ul>
    {#if usage.spools.length > 1}
      <p class="flex justify-between border-t border-border pt-2 text-sm font-medium">
        <span>{t('materials.job.total')}</span>
        <span class="tabular-nums">{grams(usage.grams)}</span>
      </p>
    {/if}
  {/if}
</div>
