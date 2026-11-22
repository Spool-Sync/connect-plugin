<!--
  What a material is, as a grid of small labelled values under its brand and
  type: its density and diameter where SpoolSync holds them for a custom
  material, and its colour. SpoolSync keeps no temperatures, hardness or tags,
  so none are shown.

  A grid of its own rather than the host's KeyValueList: inside a module, the
  module's own utilities are scoped and outrank the host's responsive ones, so
  the list's rows never turned into rows (Core moduleBuild scoping).

  The add dialog shows it too. The columns fill whatever width they get, the
  page's or the dialog's.
-->
<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';
  import { MaterialSwatch } from '@prusa/connect-ui/primitives';

  import type { Detail } from '../api';
  import MaterialIcon from '../material-icon/material-icon.svelte';
  import { heldHost } from '../host';

  let {
    detail,
    bare = false,
    wrapValues = false,
  }: {
    detail: Detail;
    /** Only the specs: the add dialog names the material above them. */
    bare?: boolean;
    wrapValues?: boolean;
  } = $props();

  const t = heldHost().t;

  // SpoolSync's own numbers are on the custom material only; a catalogue
  // material's density and diameter are not in the contract.
  const custom = $derived(detail.custom ?? undefined);

  const rows = $derived([
    {
      id: 'density',
      label: t('materials.details.density'),
      value:
        custom?.density === undefined || custom.density === null
          ? undefined
          : t('materials.details.densityValue', {
              density: formatNumber(custom.density),
            }),
    },
    {
      id: 'diameter',
      label: t('materials.details.diameter'),
      value:
        custom?.diameter_um === undefined || custom.diameter_um === null
          ? undefined
          : t('materials.details.diameterValue', {
              diameter: formatNumber(custom.diameter_um / 1000),
            }),
    },
    {
      id: 'colour',
      label: t('materials.details.colour'),
      value: detail.color_rgba === '' ? undefined : detail.color_rgba,
      mono: true,
    },
  ]);

  const shown = $derived(rows.filter((row) => row.value !== undefined));
</script>

<div data-part="material-details" class="flex items-start gap-4">
  {#if !bare}
    <MaterialIcon
      class="size-28"
      materialClass={detail.class ?? 'FFF'}
      colorRgba={detail.color_rgba}
    />
  {/if}
  <div class="flex min-w-0 flex-1 flex-col gap-4">
    {#if !bare}
      <p class="text-sm text-muted-foreground">
        {[detail.brand_name, detail.type, detail.class]
          .filter((s) => s !== undefined && s !== '')
          .join(' · ')}
      </p>
    {/if}
    {#if shown.length > 0}
      <dl class={['specs grid gap-x-8 gap-y-4', { 'wrap-values': wrapValues }]}>
        {#each shown as row (row.id)}
          <div class="flex min-w-0 flex-col gap-1">
            <dt class="text-xs tracking-wide text-muted-foreground uppercase">
              {row.label}
            </dt>
            <dd
              class={[
                'flex min-w-0 items-center gap-2 text-sm font-medium',
                row.mono ? 'font-mono' : 'tabular-nums',
              ]}
            >
              {#if row.id === 'colour'}
                <MaterialSwatch
                  class="size-4 shrink-0"
                  material={{ name: detail.name ?? '', colorRgba: detail.color_rgba }}
                />
              {/if}
              <span class="min-w-0 break-words">{row.value}</span>
            </dd>
          </div>
        {/each}
      </dl>
    {/if}
  </div>
</div>

<style>
  .specs.wrap-values {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .specs {
    grid-template-columns: repeat(
      auto-fill,
      minmax(calc(var(--spacing) * 36), 1fr)
    );
  }
</style>
