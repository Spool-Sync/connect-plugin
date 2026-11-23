<!--
  Makes a spool the way SpoolSync's own form does: a manufacturer, then a
  material type, then a material of that brand and type, each narrowing the
  next. The materials are the catalogue's colours of it, and a colour the
  catalogue lacks is added as a custom material right there, then picked.

  The weights are the spool's full weight and what is left on it; the current
  weight follows the initial one until the person types their own. Where the
  spool is kept and any notes come last. A refusal is drawn in the dialog and
  the form is kept, so nothing typed is lost.
-->
<script lang="ts">
  import PlusIcon from '@lucide/svelte/icons/plus';
  import { Button, ColorInput, Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, Field, Input, Label, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Textarea, toast, Combobox } from '@prusa/connect-ui/primitives';
  import { untrack } from 'svelte';

  import { type Brand, type Row, materialsOn } from '../api';
  import { cssColour } from '../css-colour';
  import { heldHost } from '../host';
  import LocationField from '../location-field.svelte';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { typedWeight } from '../materials-page/weights';
  import { OWN_MODULE } from '../own-module';

  let {
    open,
    onclose,
    oncreated,
    title,
    type: presetType,
  }: {
    open: boolean;
    onclose: () => void;
    /** The spool made, by its id, before the dialog closes. */
    oncreated: (id: string) => void;
    /** The dialog's heading; Create spool when left out. */
    title?: string;
    /** The material type to begin with, such as the one a printer reports. */
    type?: string | undefined;
  } = $props();

  const host = heldHost();
  const t = host.t;
  const service = materialsOn(host);
  const uid = $props.id();

  /** How many catalogue colours one choice reads. The module answers 100 at most. */
  const LIMIT = 100;
  /** The default colour of a custom one, until the person picks another. */
  const FIRST_COLOUR = '#808080';

  let brands = $state.raw<readonly Brand[]>([]);
  let types = $state.raw<readonly string[]>([]);
  let manufacturer = $state('');
  let type = $state('');
  /** The catalogue's colours of this manufacturer and type. */
  let catalogue = $state.raw<readonly Row[]>([]);
  /** The custom colours added here, for this manufacturer and type. */
  let added = $state.raw<readonly Row[]>([]);
  let chosen = $state<string | undefined>(undefined);
  let reading = $state(false);
  let adding = $state(false);
  let colourName = $state('');
  let colour = $state(FIRST_COLOUR);
  let initialText = $state('');
  let currentText = $state('');
  /** Once the person types a current weight, the initial one stops setting it. */
  let currentTyped = false;
  let locationId = $state<string | undefined>(undefined);
  let notes = $state('');
  let saving = $state(false);
  let fault = $state.raw<unknown>(undefined);
  /** Each choice of manufacturer and type asks again; an older answer is dropped. */
  let turn = 0;

  const initial = $derived(typedWeight(initialText));
  const current = $derived(
    currentText.trim() === '' ? initial : typedWeight(currentText),
  );
  const weightsOk = $derived(
    initial !== undefined &&
      Number.isFinite(initial) &&
      initial > 0 &&
      current !== undefined &&
      Number.isFinite(current) &&
      current >= 0 &&
      current <= initial,
  );
  const swatches = $derived([...added, ...catalogue]);
  const ready = $derived(
    chosen !== undefined && weightsOk && !saving,
  );

  const brandItems = $derived.by(() => {
    const listed = brands.map((b) => ({ value: b.name, label: b.name }));
    const typed = manufacturer.trim();
    if (typed !== '' && !brands.some((b) => b.name === typed)) {
      listed.push({ value: typed, label: typed });
    }

    return listed;
  });
  const typeItems = $derived(types.map((x) => ({ value: x, label: x })));
  /** A manufacturer the catalogue names is searched; a new name has no catalogue colours. */
  const slug = (name: string): string | undefined =>
    brands.find((b) => b.name === name.trim())?.slug;

  const reset = (): void => {
    turn += 1;
    manufacturer = '';
    type = presetType ?? '';
    catalogue = [];
    added = [];
    chosen = undefined;
    reading = false;
    adding = false;
    colourName = '';
    colour = FIRST_COLOUR;
    initialText = '';
    currentText = '';
    currentTyped = false;
    locationId = undefined;
    notes = '';
    saving = false;
    fault = undefined;
  };

  const readChoices = async (): Promise<void> => {
    try {
      const [brandList, typeList] = await Promise.all([
        service.brands(),
        service.types(),
      ]);
      brands = brandList;
      types = typeList;
    } catch (thrown) {
      fault = thrown;
    }
  };

  // Each time the dialog opens it starts empty and reads the choices again, so
  // a manufacturer or a type added since the last time is there.
  $effect(() => {
    if (!open) return;
    untrack(() => {
      reset();
      void readChoices();
    });
  });

  /** Reads the catalogue's colours of the manufacturer and type picked. */
  const loadColours = async (): Promise<void> => {
    const current = ++turn;
    catalogue = [];
    added = [];
    chosen = undefined;
    adding = false;
    fault = undefined;
    const brandSlug = slug(manufacturer);
    if (brandSlug === undefined || type === '') {
      reading = false;
      return;
    }
    reading = true;
    try {
      const page = await service.list({
        brand: brandSlug,
        type,
        class: 'FFF',
        limit: LIMIT,
      });
      if (current === turn) catalogue = page.rows;
    } catch (thrown) {
      if (current === turn) fault = thrown;
    } finally {
      if (current === turn) reading = false;
    }
  };

  const setManufacturer = (name: string): void => {
    const next = name.trim();
    if (next === manufacturer) return;
    manufacturer = next;
    void loadColours();
  };

  const setType = (next: string): void => {
    if (next === type) return;
    type = next;
    void loadColours();
  };

  const typed = (event: Event): string =>
    (event.currentTarget as HTMLInputElement).value;

  const addColour = async (): Promise<void> => {
    const name = colourName.trim();
    if (name === '' || saving) return;
    const brand = manufacturer.trim();
    const rgba = `${colour.toLowerCase()}ff`;
    saving = true;
    fault = undefined;
    try {
      const uuid = await service.createCustom({
        name,
        brand_name: brand,
        class: 'FFF',
        type,
        color_rgba: rgba,
        diameter_um: 1750,
      });
      added = [
        ...added,
        {
          uuid,
          name,
          brand_name: brand,
          class: 'FFF',
          type,
          color_rgba: rgba,
          is_favourite: false,
          is_custom: true,
          spools: 0,
          assigned_spools: 0,
          remaining_weight_g: 0,
          free_g: 0,
        },
      ];
      chosen = uuid;
      adding = false;
      colourName = '';
      colour = FIRST_COLOUR;
    } catch (thrown) {
      fault = thrown;
    } finally {
      saving = false;
    }
  };

  const submit = async (event: SubmitEvent): Promise<void> => {
    event.preventDefault();
    const material = chosen;
    const full = initial;
    const left = current;
    if (!ready || material === undefined || full === undefined || left === undefined) {
      return;
    }
    saving = true;
    fault = undefined;
    try {
      const { added: count, ids } = await service.addSpools({
        material_uuid: material,
        full_weight_g: full,
        remaining_weight_g: left,
        notes: notes.trim(),
        quantity: 1,
        ...(locationId === undefined ? {} : { location_id: locationId }),
      });
      toast.success(t('materials.add.created', { n: count }));
      const [id] = ids;
      if (id !== undefined) oncreated(id);
      onclose();
    } catch (thrown) {
      fault = thrown;
    } finally {
      saving = false;
    }
  };
</script>

<Dialog
  {open}
  onOpenChange={(next: boolean) => {
    if (!next) onclose();
  }}
>
  <!-- Portalled, so the panel carries the module's name for the utilities
       inside it. -->
  <DialogContent
    data-module={OWN_MODULE}
    size="lg"
    class="max-h-dialog-height overflow-y-auto"
  >
    <DialogHeader>
      <DialogTitle>{title ?? t('materials.create.title')}</DialogTitle>
    </DialogHeader>
    {#if open}
      <form class="flex flex-col gap-4" onsubmit={(event) => void submit(event)}>
        <Field
          label={`${t('materials.create.brand')} *`}
          control={Combobox}
          controlProps={{
            items: brandItems,
            value: manufacturer === '' ? undefined : manufacturer,
            label: t('materials.create.brand'),
            placeholder: t('materials.create.brandPlaceholder'),
            createLabel: (text: string) => t('materials.create.brandCreate', { name: text }),
            onCreate: (text: string) => setManufacturer(text),
            onPick: (picked: string | undefined) => setManufacturer(picked ?? ''),
          }}
        />

        <div class="flex flex-col gap-2">
          <Label for="{uid}-type">{`${t('materials.create.type')} *`}</Label>
          <Select
            value={type}
            items={typeItems}
            disabled={manufacturer === ''}
            onValueChange={(next: string) => setType(next)}
          >
            <SelectTrigger
              id="{uid}-type"
              class="w-full"
              aria-label={t('materials.create.type')}
            >
              <SelectValue placeholder={t('materials.create.typePlaceholder')} />
            </SelectTrigger>
            <SelectContent>
              {#each typeItems as item (item.value)}
                <SelectItem value={item.value} label={item.label}>
                  {item.label}
                </SelectItem>
              {/each}
            </SelectContent>
          </Select>
        </div>

        <div class="flex flex-col gap-2" role="group" aria-labelledby="{uid}-material">
          <p id="{uid}-material" class="text-sm font-medium">
            {t('materials.create.material')}
          </p>
          {#if manufacturer === '' || type === ''}
            <p class="text-sm text-muted-foreground">
              {t('materials.create.materialHint')}
            </p>
          {:else if reading}
            <p class="text-sm text-muted-foreground">
              {t('materials.create.reading')}
            </p>
          {:else if swatches.length === 0}
            <p class="text-sm text-muted-foreground">
              {t('materials.create.noColours')}
            </p>
          {/if}

          <div class="flex flex-wrap gap-2">
            {#each swatches as row (row.uuid)}
              {@const shade = cssColour(row.color_rgba)}
              <button
                type="button"
                data-part="colour"
                aria-pressed={chosen === row.uuid}
                class={[
                  'flex w-20 flex-col items-center gap-1 rounded-sm border p-2 text-center text-xs outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring',
                  chosen === row.uuid ? 'border-primary bg-primary/10' : 'border-transparent',
                ]}
                onclick={() => (chosen = row.uuid)}
              >
                <span
                  class="size-8 rounded-full border border-border"
                  style:background-color={shade}
                  aria-hidden="true"
                ></span>
                <span class="line-clamp-2 w-full break-words">{row.name}</span>
              </button>
            {/each}

            {#if manufacturer !== '' && type !== '' && !adding}
              <button
                type="button"
                class="flex w-20 flex-col items-center justify-center gap-1 rounded-sm border border-dashed border-border p-2 text-xs text-primary outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"
                onclick={() => (adding = true)}
              >
                <PlusIcon class="size-4" aria-hidden="true" />
                {t('materials.create.addColour')}
              </button>
            {/if}
          </div>

          {#if adding}
            <!-- A colour the catalogue lacks, made as a custom material of this
                 manufacturer and type, and picked at once. -->
            <div class="flex flex-wrap items-end gap-2 rounded-sm border border-border p-3">
              <div class="flex min-w-40 flex-1 flex-col gap-2">
                <Label for="{uid}-colour-name">{t('materials.create.colourName')}</Label>
                <Input
                  id="{uid}-colour-name"
                  value={colourName}
                  placeholder={t('materials.create.colourNamePlaceholder')}
                  oninput={(event: Event) => (colourName = typed(event))}
                />
              </div>
              <ColorInput
                label={t('materials.add.colour')}
                value={colour}
                oninput={(hex: string) => (colour = hex)}
              />
              <Button
                type="button"
                size="sm"
                disabled={colourName.trim() === '' || saving}
                onclick={() => void addColour()}
              >
                {t('materials.create.addColour')}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onclick={() => (adding = false)}
              >
                {t('materials.add.cancel')}
              </Button>
            </div>
          {/if}
        </div>

        <div class="grid grid-cols-2 gap-4">
          <Field
            label={`${t('materials.create.initial')} *`}
            control={Input}
            controlProps={{
              name: 'initial_weight_g',
              type: 'text',
              inputmode: 'decimal',
              value: initialText,
              oninput: (event: Event) => {
                initialText = typed(event);
                if (!currentTyped) currentText = initialText;
              },
            }}
          />
          <Field
            label={t('materials.create.current')}
            control={Input}
            controlProps={{
              name: 'current_weight_g',
              type: 'text',
              inputmode: 'decimal',
              value: currentText,
              oninput: (event: Event) => {
                currentText = typed(event);
                currentTyped = true;
              },
            }}
          />
        </div>

        <LocationField
          value={locationId}
          onpick={(id: string | undefined) => (locationId = id)}
        />

        <div class="flex flex-col gap-2">
          <Label for="{uid}-notes">{t('materials.spool.notes')}</Label>
          <Textarea id="{uid}-notes" rows={2} bind:value={notes} />
        </div>

        <FaultNote {fault} />

        <DialogFooter>
          <Button type="button" variant="secondary" onclick={onclose}>
            {t('materials.add.cancel')}
          </Button>
          <Button type="submit" disabled={!ready}>
            {t('materials.create.submit')}
          </Button>
        </DialogFooter>
      </form>
    {/if}
  </DialogContent>
</Dialog>
