<!--
  The fields of a custom material the contract holds: what the material is (its
  class, manufacturer, type, name and abbreviation), its primary colour, and its
  density. It sends what was typed to `onsave` and makes no request itself, so
  the add dialog and the Custom material dialog create with it and the material
  page edits with it: `initial` fills the fields and `submitLabel` names the
  button.

  The manufacturer, the name and the primary colour are required, and the type
  too unless the material is a resin; the button stays off until they are
  filled. A preset fills a filament's density, still editable. The name follows
  the type and the manufacturer until the person writes one of their own.

  A number left empty is left out, which the module keeps as unknown. Temperatures,
  nozzle sizes, hardness and certifications are not in the SpoolSync contract, so
  this form does not ask for them.
-->
<script lang="ts">
  import {
    Button,
    Chip,
    ColorInput,
    Combobox,
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
    DialogFooter,
    Field,
    Input,
    SwatchPicker,
  } from '@prusa/connect-ui/primitives';
  import { onMount, untrack } from 'svelte';

  import type { MaterialFields } from '../api';
  import { heldHost } from '../host';
  import { heldList } from '../list.svelte';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { PALETTE, PRESETS } from './custom-options';

  let {
    initial,
    filamentOnly = false,
    compact = false,
    submitLabel,
    busy,
    fault,
    onsave,
    oncancel,
  }: {
    initial?: MaterialFields | undefined;
    filamentOnly?: boolean;
    compact?: boolean;
    submitLabel: string;
    busy: boolean;
    fault: unknown;
    onsave: (fields: MaterialFields) => void;
    oncancel: () => void;
  } = $props();

  const t = heldHost().t;
  const list = heldList();

  // The form is made new each time it is shown, so it starts from `initial`.
  const start = untrack(() => initial);
  let klass = $state<'FFF' | 'SLA'>(
    !untrack(() => filamentOnly) && start?.class === 'SLA' ? 'SLA' : 'FFF',
  );
  let preset = $state<string | undefined>(undefined);
  let brand = $state(start?.brand_name ?? '');
  let type = $state(start?.type ?? '');
  let name = $state(start?.name ?? '');
  let abbreviation = $state(start?.abbreviation ?? '');
  let primary = $state<string | undefined>(
    start?.color_rgba ? start.color_rgba.slice(0, 7) : undefined,
  );
  // The picker has no alpha, so an edit keeps the one the material had.
  const alpha = (start?.color_rgba ?? '').slice(7, 9);
  let density = $state(start?.density === undefined ? '' : String(start.density));

  onMount(() => {
    void list.readFacets();
  });

  const resin = $derived(klass === 'SLA');

  // The name follows the type and the manufacturer, "PLA Acme", until the
  // person writes one of their own: a name that is not the last one made
  // here is theirs.
  let made = '';
  const composed = (): string => `${type} ${brand.trim()}`.trim();
  const follow = (): void => {
    if (name.trim() !== '' && name !== made) return;
    made = composed();
    name = made;
  };

  const typeOptions = $derived(
    [...new Set([...list.types, ...PRESETS.map((p) => p.type)])]
      .sort()
      .map((value) => ({ value, label: value })),
  );

  // Catalogue manufacturers while the typed one is not one of them: picking
  // one writes its name as the catalogue spells it.
  const suggestions = $derived.by(() => {
    const query = brand.trim().toLowerCase();
    if (
      query === '' ||
      list.brands.some((b) => b.name.toLowerCase() === query)
    ) {
      return [];
    }

    return list.brands
      .filter((b) => b.name.toLowerCase().includes(query))
      .slice(0, 5);
  });

  const missing = $derived(
    brand.trim() === '' ||
      name.trim() === '' ||
      primary === undefined ||
      (!resin && type === ''),
  );

  const applyPreset = (chosen: (typeof PRESETS)[number]): void => {
    preset = chosen.type;
    type = chosen.type;
    density = chosen.values.density === undefined ? '' : String(chosen.values.density);
    follow();
  };

  const typed = (event: Event): string =>
    (event.currentTarget as HTMLInputElement).value;

  const submit = (event: SubmitEvent): void => {
    event.preventDefault();
    if (missing || busy || primary === undefined) return;
    const value = Number(density);
    onsave({
      name: name.trim(),
      class: klass,
      brand_name: brand.trim(),
      type: resin ? '' : type,
      abbreviation: abbreviation.trim(),
      color_rgba: `${primary}${alpha}`,
      ...(density.trim() === '' || !Number.isFinite(value)
        ? {}
        : { density: value }),
      // A diameter the form does not ask keeps what the material had.
      ...(start?.diameter_um === undefined
        ? {}
        : { diameter_um: start.diameter_um }),
    });
  };

  const required = (label: string): string => `${label} *`;

  const PRIMARY_SWATCHES = PALETTE.map((hex) => ({
    key: hex,
    hex,
    label: hex,
  }));
</script>

{#snippet identity()}
  {#if !filamentOnly}
    <div
      class="flex flex-wrap gap-2"
      role="group"
      aria-label={t('materials.filter.class')}
    >
      {#each ['FFF', 'SLA'] as const as chosen (chosen)}
        <Chip
          selected={klass === chosen}
          onclick={() => {
            klass = chosen;
            // A resin has no filament type, so neither the type nor a preset
            // built around one survives the switch.
            if (chosen === 'SLA') {
              type = '';
              preset = undefined;
            }
            follow();
          }}
        >
          {chosen}
        </Chip>
      {/each}
    </div>
  {/if}

  {#if !resin}
    <div class="flex flex-col gap-2">
      <p class="text-sm font-medium">{t('materials.custom.presets')}</p>
      {#if !compact}
        <p class="text-xs text-muted-foreground">
          {t('materials.custom.presetsHint')}
        </p>
      {/if}
      <div
        class="flex flex-wrap gap-2"
        role="group"
        aria-label={t('materials.custom.presets')}
      >
        {#each PRESETS as p (p.type)}
          <Chip selected={preset === p.type} onclick={() => applyPreset(p)}>
            {p.type}
          </Chip>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Two fields to a row where the dialog has the width for it. -->
  <div class={['pair grid', compact ? 'compact-pair gap-2' : 'gap-4']}>
    <div class="flex flex-col gap-2">
      <Field
        label={required(t('materials.custom.brand'))}
        control={Input}
        controlProps={{
          name: 'brand_name',
          autocomplete: 'off',
          placeholder: t('materials.custom.brandPlaceholder'),
          value: brand,
          oninput: (e: Event) => {
            brand = typed(e);
            follow();
          },
        }}
      />
      {#if suggestions.length > 0}
        <div
          class="flex flex-wrap gap-2"
          role="group"
          aria-label={t('materials.custom.brandSuggestions')}
        >
          {#each suggestions as suggestion (suggestion.uuid)}
            <Chip
              onclick={() => {
                brand = suggestion.name;
                follow();
              }}
            >
              {suggestion.name}
            </Chip>
          {/each}
        </div>
      {/if}
    </div>
    {#if !resin}
      <Field
        label={required(t('materials.custom.type'))}
        control={Combobox}
        controlProps={{
          items: typeOptions,
          value: type === '' ? undefined : type,
          label: t('materials.custom.type'),
          placeholder: t('materials.custom.typePlaceholder'),
          onPick: (picked: string | undefined) => {
            type = picked ?? '';
            // A preset only stands for as long as its own type does.
            if (preset !== type) preset = undefined;
            follow();
          },
        }}
      />
    {/if}
    <Field
      label={required(t('materials.custom.name'))}
      class={compact ? 'col-span-full' : undefined}
      control={Input}
      controlProps={{
        name: 'name',
        autocomplete: 'off',
        placeholder: composed() || t('materials.custom.namePlaceholder'),
        value: name,
        oninput: (e: Event) => (name = typed(e)),
      }}
    />
    {#if !compact}
      {@render abbreviationField()}
    {/if}
  </div>
{/snippet}

{#snippet abbreviationField()}
  <Field
    label={t('materials.custom.abbreviation')}
    control={Input}
    controlProps={{
      name: 'abbreviation',
      autocomplete: 'off',
      placeholder: type,
      value: abbreviation,
      oninput: (e: Event) => (abbreviation = typed(e)),
    }}
  />
{/snippet}

{#snippet colourField()}
  <div class="flex flex-col gap-2">
    <p class="text-sm font-medium">
      {required(t('materials.custom.primary'))}
    </p>
    <div class="flex flex-wrap items-center gap-2">
      <SwatchPicker
        swatches={PRIMARY_SWATCHES}
        value={primary}
        label={t('materials.custom.primary')}
        size={compact ? 'sm' : 'md'}
        onPick={(hex: string | undefined) => {
          if (hex !== undefined) primary = hex;
        }}
      />
      <ColorInput
        label={t('materials.custom.primaryCustom')}
        value={primary ?? '#000000'}
        oninput={(hex: string) => (primary = hex)}
      />
      {#if primary !== undefined}
        <Chip onclick={() => (primary = undefined)}>{primary} ✕</Chip>
      {/if}
    </div>
  </div>
{/snippet}

{#snippet physical()}
  <div class="pair grid gap-4">
    <Field
      label={t('materials.custom.density')}
      control={Input}
      controlProps={{
        name: 'density',
        type: 'number',
        inputmode: 'decimal',
        step: 'any',
        value: density,
        oninput: (e: Event) => (density = typed(e)),
      }}
    />
  </div>
{/snippet}

<form
  data-part="custom-material"
  class={['flex flex-col', compact ? 'gap-3' : 'gap-4']}
  onsubmit={submit}
>
  <FaultNote {fault} />
  {#if compact}
    {@render identity()}
    {@render colourField()}
    <Collapsible class="rounded-sm border border-border">
      <CollapsibleTrigger
        type="button"
        class="cursor-pointer px-3 py-2 text-sm font-medium hover:bg-accent"
      >
        {t('materials.custom.advanced')}
      </CollapsibleTrigger>
      <CollapsibleContent
        forceMount
        class="compact-options grid gap-4 border-t border-border p-3"
      >
        {@render abbreviationField()}
        {@render physical()}
      </CollapsibleContent>
    </Collapsible>
  {:else}
    <div class="flex flex-col gap-6 divide-y divide-border">
      <div class="flex flex-col gap-4">
        {@render identity()}
      </div>
      <div class="flex flex-col gap-4 pt-6">
        {@render colourField()}
      </div>
      <div class="flex flex-col gap-4 pt-6">
        <h3 class="text-sm font-semibold">{t('materials.custom.physical')}</h3>
        {@render physical()}
      </div>
    </div>
  {/if}
  <DialogFooter>
    <Button type="button" variant="secondary" onclick={oncancel}>
      {t('materials.add.cancel')}
    </Button>
    <Button
      type="submit"
      disabled={missing || busy}
      title={missing ? t('materials.custom.incomplete') : undefined}
    >
      {submitLabel}
    </Button>
  </DialogFooter>
</form>

<style>
  :global(.compact-options[data-state='closed']) {
    display: none;
  }

  .pair.compact-pair {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .pair {
    grid-template-columns: repeat(
      auto-fit,
      minmax(calc(var(--spacing) * 48), 1fr)
    );
  }
</style>
