<!--
  Step 2's fields: how many spools, the weights, what is left, where they are
  kept, and a summary of what the submit will make.

  Every field writes straight onto the draft, which is what the dialog posts.
  SpoolSync holds no packages, so there is nothing to choose a weight from:
  the person types the weights.
-->
<script lang="ts">
  import {
    Field,
    Input,
    Label,
    Textarea,
  } from '@prusa/connect-ui/primitives';

  import { heldHost } from '../host';
  import LocationField from '../location-field.svelte';
  import { typedWeight } from '../materials-page/weights';
  import type { SpoolDraftForm } from './draft.svelte';

  let {
    draft,
    section = 'all',
  }: { draft: SpoolDraftForm; section?: 'all' | 'remaining' | 'optional' } =
    $props();

  const t = heldHost().t;
  const uid = $props.id();


  const typed = (event: Event): string =>
    (event.currentTarget as HTMLInputElement).value;
  /** A number field: empty is unknown. */
  const grams = (event: Event): number | undefined => {
    const value = typed(event);

    return value === '' ? undefined : Number(value);
  };
  const weight = (event: Event): number | undefined =>
    typedWeight(typed(event));
  const weightProps = (value: number | undefined) => ({
    type: 'text',
    inputmode: 'decimal',
    value: value ?? '',
  });
</script>

{#snippet remaining()}
  <Field
    label={t('materials.spool.remaining')}
    control={Input}
    controlProps={{
      name: 'remaining_weight_g',
      ...weightProps(draft.remainingG),
      oninput: (e: Event) => (draft.remainingG = weight(e)),
    }}
  />
{/snippet}

{#if section === 'remaining'}
  {@render remaining()}
{:else}
  <div
    data-part="spool-details"
    class={['fields grid gap-4', { compact: section === 'optional' }]}
  >

    {#if section === 'all'}
      <Field
        label={t('materials.spool.quantity')}
        control={Input}
        controlProps={{
          name: 'quantity',
          type: 'number',
          inputmode: 'numeric',
          min: 1,
          max: 100,
          step: 1,
          required: true,
          value: draft.quantity,
          oninput: (e: Event) => (draft.quantity = grams(e) ?? 0),
        }}
      />
    {/if}

    <Field
      label={t('materials.spool.full')}
      control={Input}
      controlProps={{
        name: 'full_weight_g',
        ...weightProps(draft.fullWeightG),
        oninput: (e: Event) => (draft.fullWeightG = weight(e)),
      }}
    />
    <Field
      label={t('materials.spool.empty')}
      control={Input}
      controlProps={{
        name: 'empty_weight_g',
        ...weightProps(draft.emptyWeightG),
        oninput: (e: Event) => (draft.emptyWeightG = weight(e)),
      }}
    />

    {#if section === 'all'}
      {@render remaining()}
    {/if}

    <LocationField
      class="col-span-full"
      value={draft.locationId}
      onpick={(id: string | undefined) => (draft.locationId = id)}
    />
    <div class="col-span-full flex flex-col gap-2">
      <Label for="{uid}-notes">{t('materials.spool.notes')}</Label>
      <Textarea id="{uid}-notes" rows={2} bind:value={draft.notes} />
    </div>

    {#if section === 'all'}
      <dl
        data-part="summary"
        class="col-span-full grid grid-cols-2 gap-x-4 gap-y-1 rounded-sm bg-muted p-3 text-sm"
      >
        <dt class="text-muted-foreground">
          {t('materials.spool.summary.count')}
        </dt>
        <dd>{Number.isFinite(draft.quantity) ? draft.quantity : '–'}</dd>
        <dt class="text-muted-foreground">
          {t('materials.spool.summary.each')}
        </dt>
        <dd>
          {draft.remaining === undefined
            ? '–'
            : t('materials.spool.grams', { g: draft.remaining })}
        </dd>
        <dt class="text-muted-foreground">
          {t('materials.spool.summary.total')}
        </dt>
        <dd>
          {draft.remaining === undefined || !Number.isFinite(draft.quantity)
            ? '–'
            : t('materials.spool.grams', {
                g: draft.remaining * draft.quantity,
              })}
        </dd>
      </dl>
    {/if}
  </div>
{/if}

<style>
  .fields.compact {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .fields {
    grid-template-columns: repeat(
      auto-fit,
      minmax(calc(var(--spacing) * 56), 1fr)
    );
  }
</style>
