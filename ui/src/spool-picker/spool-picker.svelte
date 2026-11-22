<!--
  Chooses one spool of SpoolSync, as a list: the search narrows it by brand,
  name, type and location, and a row of manufacturer chips and a row of
  material-type chips narrow it further. The type chips are the types of the
  chosen manufacturer only. Clicking the selected row lets the choice go.

  Spools on a tool come after the rest: a spool a printer already holds is the
  one a person means least often. The picker keeps no choice of its own; the
  host hands the id in as `selected` and takes the next one from `onselect`.
-->
<script lang="ts">
  import CheckIcon from '@lucide/svelte/icons/check';
  import PackageXIcon from '@lucide/svelte/icons/package-x';
  import { formatNumber } from '@prusa/connect-ui/i18n';
  import { Chip, ListingSearch } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import type { Spool } from '../api';
  import { cssColour } from '../css-colour';
  import { heldHost } from '../host';
  import { toolLabel } from '../printer-material/tool-label';

  let {
    spools,
    selected,
    onselect,
  }: {
    /** The live spools to choose from. */
    spools: readonly Spool[];
    /** The chosen spool's id, undefined when none is chosen. */
    selected: string | undefined;
    /** The id picked, or undefined when the chosen row was picked again. */
    onselect: (id: string | undefined) => void;
  } = $props();

  const t = heldHost().t;

  let area = $state<HTMLDivElement>();
  let query = $state('');
  let brand = $state<string>();
  let type = $state<string>();

  onMount(() => {
    area?.querySelector<HTMLInputElement>('input[type="search"]')?.focus();
  });

  const byName = (a: string, b: string): number => a.localeCompare(b);

  const brands = $derived(
    [...new Set(spools.map((s) => s.brand_name).filter((x) => x !== ''))].sort(
      byName,
    ),
  );
  // A filter that the spools no longer offer stops applying, rather than
  // hiding every row behind a chip nobody can see.
  const chosenBrand = $derived(
    brand !== undefined && brands.includes(brand) ? brand : undefined,
  );
  const inBrand = $derived(
    chosenBrand === undefined
      ? spools
      : spools.filter((s) => s.brand_name === chosenBrand),
  );
  const types = $derived(
    [...new Set(inBrand.map((s) => s.type).filter((x) => x !== ''))].sort(
      byName,
    ),
  );
  const chosenType = $derived(
    type !== undefined && types.includes(type) ? type : undefined,
  );

  const needle = $derived(query.trim().toLowerCase());
  const shown = $derived.by(() => {
    const matching = inBrand.filter((s) => {
      if (chosenType !== undefined && s.type !== chosenType) return false;
      if (needle === '') return true;

      return `${s.brand_name} ${s.material_name} ${s.type} ${s.location}`
        .toLowerCase()
        .includes(needle);
    });

    return [
      ...matching.filter((s) => s.tool == null),
      ...matching.filter((s) => s.tool != null),
    ];
  });

  /** What a row says where the spool is: the tool it is on, or its location. */
  const where = (s: Spool): string | undefined => {
    if (s.tool != null) {
      return t('materials.spoolPicker.onTool', {
        tool: toolLabel(t, s.tool.tool_id),
      });
    }

    return s.location === '' ? undefined : s.location;
  };

  const pick = (s: Spool): void => {
    onselect(selected === s.id ? undefined : s.id);
  };
</script>

<div bind:this={area} class="flex min-w-0 flex-col gap-3">
  <ListingSearch
    value={query}
    onValue={(next: string) => (query = next)}
    label={t('materials.spoolPicker.search')}
    placeholder={t('materials.spoolPicker.searchPlaceholder')}
  />

  {#if brands.length > 0}
    <div
      class="flex flex-wrap gap-2"
      role="group"
      aria-label={t('materials.spoolPicker.brands')}
    >
      {#each brands as name (name)}
        <Chip
          selected={chosenBrand === name}
          onclick={() => (brand = chosenBrand === name ? undefined : name)}
        >
          {name}
        </Chip>
      {/each}
    </div>
  {/if}

  {#if types.length > 0}
    <div
      class="flex flex-wrap gap-2"
      role="group"
      aria-label={t('materials.spoolPicker.types')}
    >
      {#each types as name (name)}
        <Chip
          selected={chosenType === name}
          onclick={() => (type = chosenType === name ? undefined : name)}
        >
          {name}
        </Chip>
      {/each}
    </div>
  {/if}

  {#if shown.length === 0}
    <div
      class="flex flex-col items-center gap-2 py-6 text-center text-sm text-muted-foreground"
    >
      <PackageXIcon class="size-8" aria-hidden="true" />
      <p>
        {spools.length > 0
          ? t('materials.spoolPicker.noMatch')
          : t('materials.spoolPicker.none')}
      </p>
    </div>
  {:else}
    <ul
      class="spool-list -mx-1 flex max-h-[340px] flex-col gap-1 overflow-y-auto px-1"
      aria-label={t('materials.spoolPicker.list')}
    >
      {#each shown as s (s.id)}
        {@const colour = cssColour(s.color_rgba)}
        {@const isSelected = selected === s.id}
        {@const place = where(s)}
        <li>
          <button
            type="button"
            data-part="spool-row"
            data-spool={s.id}
            aria-pressed={isSelected}
            class={[
              'flex w-full items-center gap-3 rounded-sm border p-2 text-start outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring',
              isSelected ? 'border-primary bg-primary/10' : 'border-border',
            ]}
            onclick={() => pick(s)}
          >
            <span
              class={['swatch', colour === undefined && 'unknown']}
              style:background-color={colour}
              aria-hidden="true"
            ></span>
            <span class="flex min-w-0 flex-1 flex-col gap-1">
              <span class="truncate text-sm font-medium">
                {s.brand_name} {s.material_name}
              </span>
              <span
                class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground"
              >
                {#if s.type !== ''}
                  <span class="rounded-full border border-border px-2 py-0.5">
                    {s.type}
                  </span>
                {/if}
                <span class="tabular-nums">
                  {t('materials.printer.grams', {
                    g: formatNumber(Math.round(s.remaining_weight_g), {
                      maximumFractionDigits: 0,
                    }),
                  })}
                </span>
                {#if place !== undefined}
                  <span class="truncate">{place}</span>
                {/if}
              </span>
            </span>
            {#if isSelected}
              <CheckIcon class="size-4 shrink-0 text-primary" aria-hidden="true" />
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .swatch {
    width: calc(var(--spacing) * 7);
    height: calc(var(--spacing) * 7);
    flex-shrink: 0;
    border-radius: 50%;
    border: 1px solid color-mix(in oklab, currentColor 18%, transparent);
  }

  /* A spool with no colour gets a rainbow ring, so it is not taken for black. */
  .swatch.unknown {
    background: conic-gradient(
      red,
      orange,
      yellow,
      lime,
      cyan,
      blue,
      magenta,
      red
    );
  }
</style>
