<!--
  Asked when the printer's NFC tag holds no full weight, so the module will not
  add the spool without one. The person says what is left on it, and what it
  held new if they know. Add answers the weights back to the host, which adds
  the spool with them.
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
    Field,
    Input,
  } from '@prusa/connect-ui/primitives';

  import type { ReportedWeights } from '../api';
  import { heldHost } from '../host';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { typedWeight } from '../materials-page/weights';
  import { OWN_MODULE } from '../own-module';

  let {
    open,
    busy,
    fault,
    onsubmit,
    onclose,
  }: {
    open: boolean;
    busy: boolean;
    fault: unknown;
    onsubmit: (weights: ReportedWeights) => void;
    onclose: () => void;
  } = $props();

  const t = heldHost().t;

  let left = $state('');
  let full = $state('');

  // Each time it opens it is empty, so a second spool does not inherit the
  // weights of the first.
  $effect(() => {
    if (open) {
      left = '';
      full = '';
    }
  });

  const remaining = $derived(typedWeight(left));
  const fullG = $derived(full.trim() === '' ? undefined : typedWeight(full));
  const valid = $derived(
    remaining !== undefined &&
      Number.isFinite(remaining) &&
      remaining > 0 &&
      (fullG === undefined || (Number.isFinite(fullG) && fullG > 0)),
  );

  const submit = (event: SubmitEvent): void => {
    event.preventDefault();
    if (!valid || busy || remaining === undefined) return;
    onsubmit({
      remaining_weight_g: remaining,
      ...(fullG === undefined ? {} : { full_weight_g: fullG }),
    });
  };
</script>

<Dialog
  {open}
  onOpenChange={(next: boolean) => {
    if (!next && !busy) onclose();
  }}
>
  <DialogContent data-module={OWN_MODULE} size="md">
    <DialogHeader>
      <DialogTitle>{t('materials.weigh.title')}</DialogTitle>
      <DialogDescription>{t('materials.weigh.note')}</DialogDescription>
    </DialogHeader>
    {#if open}
      <form class="flex flex-col gap-4" onsubmit={submit}>
        <Field
          label={t('materials.weigh.left')}
          control={Input}
          controlProps={{
            name: 'remaining_weight_g',
            type: 'text',
            inputmode: 'decimal',
            required: true,
            value: left,
            oninput: (event: Event) =>
              (left = (event.currentTarget as HTMLInputElement).value),
          }}
        />
        <Field
          label={t('materials.weigh.full')}
          control={Input}
          controlProps={{
            name: 'full_weight_g',
            type: 'text',
            inputmode: 'decimal',
            value: full,
            oninput: (event: Event) =>
              (full = (event.currentTarget as HTMLInputElement).value),
          }}
        />
        <FaultNote {fault} />
        <DialogFooter>
          <Button type="button" variant="secondary" onclick={onclose}>
            {t('materials.add.cancel')}
          </Button>
          <Button type="submit" disabled={!valid || busy}>
            {t('materials.weigh.add')}
          </Button>
        </DialogFooter>
      </form>
    {/if}
  </DialogContent>
</Dialog>
