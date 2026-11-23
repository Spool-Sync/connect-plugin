<!--
  The stock at a glance: SpoolSync's dashboard without its printer cards,
  reorder alerts and recently added spools. The top part, above the list, is
  the totals, the filament over the last 30 days and the share of each
  material; every box there has a fixed height, so the list below never moves
  when the numbers land. The bottom part, under the list, is a tile per
  material and the filament left of each filament type.

  A tile narrows the list to its material. The overview reads again whenever
  the list does, so a spool added or used shows in both.
-->
<script lang="ts">
  import {
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    LineChart,
    Skeleton,
    type ChartSeries,
  } from '@prusa/connect-ui/primitives';
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import type { Overview } from '../api';
  import { heldHost } from '../host';
  import { kilograms } from './weights';

  interface Props {
    /** Which half: the fixed-height top, or the tiles and bars under the list. */
    part: 'top' | 'bottom';
    overview: Overview | undefined;
    /** The material a tile narrows the list to, or none. */
    selected?: string | undefined;
    onpick?: (material: string) => void;
  }

  let { part, overview, selected, onpick }: Props = $props();

  const t = heldHost().t;

  /** SpoolSync's own material colours, and a blue for the rest. */
  const MATERIAL_COLOURS: Record<string, string> = {
    PLA: '#4CAF50',
    PETG: '#2196F3',
    ABS: '#F44336',
    ASA: '#FF9800',
    TPU: '#9C27B0',
    NYLON: '#00BCD4',
    PA: '#00BCD4',
    PC: '#795548',
    HIPS: '#607D8B',
    PVA: '#009688',
  };
  const colourOf = (material: string): string =>
    MATERIAL_COLOURS[material.toUpperCase()] ?? '#90CAF9';

  const weight = (grams: number): string =>
    grams >= 1000
      ? t('materials.overview.kg', { kg: kilograms(grams) })
      : t('materials.overview.g', { g: formatNumber(Math.round(grams)) });

  const usage = $derived<ChartSeries[]>(
    (overview?.usage ?? [])
      .filter((line) => line.points.length > 0)
      .map((line) => ({
        id: line.id,
        label: line.label,
        points: line.points.map((p) => ({
          at: Date.parse(p.date),
          value: p.grams,
        })),
      })),
  );

  /** The donut's arcs: each material's share of the spools in stock. */
  const RADIUS = 15.9155; // a circumference of 100, so a share is its dash
  const arcs = $derived.by(() => {
    const tiles = overview?.by_material ?? [];
    const total = tiles.reduce((sum, m) => sum + m.spools, 0);
    let offset = 0;
    return tiles.map((m) => {
      const share = total === 0 ? 0 : (m.spools / total) * 100;
      const arc = { material: m.material, share, offset, colour: colourOf(m.material) };
      offset += share;
      return arc;
    });
  });

  const typeMax = $derived(
    Math.max(1, ...(overview?.by_type ?? []).map((b) => b.grams)),
  );
  const stats = $derived(
    overview === undefined
      ? []
      : [
          { label: t('materials.overview.spools'), value: formatNumber(overview.spools) },
          { label: t('materials.overview.inventory'), value: weight(overview.grams) },
          { label: t('materials.overview.materials'), value: formatNumber(overview.materials) },
        ],
  );
</script>

<div data-part="overview-{part}" class="@container flex flex-col gap-4">
  {#if part === 'top'}
  <div class="grid grid-cols-1 gap-4 @md:grid-cols-3">
    {#if overview === undefined}
      {#each [0, 1, 2] as i (i)}
        <Skeleton class="h-24 rounded-card" />
      {/each}
    {:else}
      {#each stats as stat (stat.label)}
        <Card class="h-24 justify-center">
          <CardContent>
            <div class="text-3xl font-semibold tabular-nums">{stat.value}</div>
            <div class="text-sm text-muted-foreground">{stat.label}</div>
          </CardContent>
        </Card>
      {/each}
    {/if}
  </div>

  <div class="grid grid-cols-1 gap-4 @3xl:grid-cols-3">
    <Card class="@3xl:col-span-2">
      <CardHeader><CardTitle>{t('materials.overview.usage')}</CardTitle></CardHeader>
      <CardContent class="h-56">
        {#if overview === undefined}
          <Skeleton class="h-full" />
        {:else if usage.length === 0}
          <p class="py-8 text-center text-sm text-muted-foreground">
            {t('materials.overview.noHistory')}
          </p>
        {:else}
          <LineChart
            series={usage}
            unit="g"
            label={t('materials.overview.usage')}
            class="h-56"
          />
        {/if}
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>{t('materials.overview.byMaterial')}</CardTitle></CardHeader>
      <CardContent class="flex h-56 flex-col items-center gap-3 overflow-hidden">
        {#if overview === undefined}
          <Skeleton class="size-36 rounded-full" />
        {:else if arcs.length === 0}
          <p class="py-8 text-sm text-muted-foreground">{t('materials.overview.none')}</p>
        {:else}
          <svg viewBox="0 0 42 42" class="size-36 shrink-0" role="img" aria-label={t('materials.overview.byMaterial')}>
            {#each arcs as arc (arc.material)}
              <circle
                cx="21"
                cy="21"
                r={RADIUS}
                fill="transparent"
                stroke-width="6"
                style:stroke={arc.colour}
                stroke-dasharray="{arc.share} {100 - arc.share}"
                stroke-dashoffset={25 - arc.offset}
              />
            {/each}
          </svg>
          <ul class="flex flex-wrap justify-center gap-x-3 gap-y-1 text-xs">
            {#each arcs as arc (arc.material)}
              <li class="flex items-center gap-1">
                <span class="inline-block size-2.5 rounded-full" style:background={arc.colour}></span>
                {arc.material}
              </li>
            {/each}
          </ul>
        {/if}
      </CardContent>
    </Card>
  </div>

  {/if}

  {#if part === 'bottom' && overview !== undefined && overview.by_material.length > 0}
    <Card>
      <CardHeader><CardTitle>{t('materials.overview.inventoryTitle')}</CardTitle></CardHeader>
      <CardContent>
        <div class="grid grid-cols-2 gap-3 @md:grid-cols-3 @3xl:grid-cols-6">
          {#each overview.by_material as m (m.material)}
            <button
              type="button"
              aria-pressed={selected === m.material}
              class={[
                'flex flex-col items-start gap-1 rounded-card border p-3 text-left transition-colors',
                'hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring',
                selected === m.material ? 'border-primary bg-muted' : 'border-border',
              ]}
              style:border-left-color={colourOf(m.material)}
              style:border-left-width="4px"
              onclick={() => onpick?.(m.material)}
            >
              <span class="text-sm font-semibold">{m.material}</span>
              <span class="text-xl font-semibold tabular-nums">{m.spools}</span>
              <span class="text-xs text-muted-foreground">
                {t('materials.overview.tile', { spools: m.spools, weight: weight(m.grams) })}
              </span>
              <span class="flex flex-wrap gap-1">
                {#each m.colours as hex (hex)}
                  <span
                    class="inline-block size-3 rounded-full border border-black/15"
                    style:background={hex}
                  ></span>
                {/each}
              </span>
            </button>
          {/each}
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>{t('materials.overview.byType')}</CardTitle></CardHeader>
      <CardContent>
        <ul class="flex flex-col gap-2">
          {#each overview.by_type as bar (bar.id)}
            <li class="grid grid-cols-[minmax(0,14rem)_1fr_auto] items-center gap-3 text-sm @max-md:grid-cols-[minmax(0,1fr)_auto]">
              <span class="truncate" title={bar.label}>{bar.label}</span>
              <span class="h-3 rounded-full bg-muted @max-md:col-span-2 @max-md:row-start-2">
                <span
                  class="block h-full rounded-full"
                  style:width="{(bar.grams / typeMax) * 100}%"
                  style:background={bar.colour ?? 'var(--color-primary)'}
                ></span>
              </span>
              <span class="tabular-nums text-muted-foreground">{weight(bar.grams)}</span>
            </li>
          {/each}
        </ul>
      </CardContent>
    </Card>
  {/if}
</div>
