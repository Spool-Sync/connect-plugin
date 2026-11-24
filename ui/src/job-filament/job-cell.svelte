<!--
  One print's cell in the module's column of the Jobs listing: the filament it
  took, in grams. A print this module counted nothing for draws nothing, as
  the slot asks (ADR-20261123-wfw rule 7).
-->
<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import { materialsOn } from '../api';
  import { heldHost } from '../host';
  import { jobTotals } from './totals.svelte';

  let { jobId }: { jobId: string } = $props();

  const host = heldHost();
  const t = host.t;
  const totals = jobTotals(materialsOn(host));

  const grams = $derived(totals.of(jobId));
</script>

{#if typeof grams === 'number'}
  <span class="tabular-nums" data-part="job-grams">
    {t('materials.printer.grams', {
      g: formatNumber(grams, { maximumFractionDigits: grams < 10 ? 1 : 0 }),
    })}
  </span>
{/if}
