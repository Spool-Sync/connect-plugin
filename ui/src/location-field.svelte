<!--
  Where a spool is kept: a picker over the module's locations, which everyone
  shares. A name no location has offers a Create row, which makes the
  location and picks it, and No location takes the spool out of any. A name is
  changed under Settings, Extensions, Materials, never here.
-->
<script lang="ts">
  import { Combobox, Field } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { type Location, materialsOn } from './api';
  import FaultNote from './materials-page/fault-note.svelte';
  import { heldHost } from './host';

  let {
    value,
    name,
    onpick,
    class: className,
  }: {
    /** The picked location's id, none when undefined. */
    value: string | undefined;
    /** The picked location's name while the list is still being read. */
    name?: string | undefined;
    onpick: (id: string | undefined) => void;
    class?: string | undefined;
  } = $props();

  const host = heldHost();
  const t = host.t;
  const service = materialsOn(host);

  let locations = $state.raw<readonly Location[]>([]);
  let fault = $state.raw<unknown>(undefined);

  const items = $derived.by(() => {
    const listed = locations.map((l) => ({
      value: String(l.id),
      label: l.name,
    }));
    if (value !== undefined && name && !locations.some((l) => l.id === value)) {
      listed.push({ value: String(value), label: name });
    }

    return listed;
  });

  // A list that fails to read leaves a field that can still create a location.
  onMount(() => {
    service
      .locations()
      .then((read) => (locations = read))
      .catch(() => undefined);
  });

  const create = async (text: string): Promise<void> => {
    fault = undefined;
    try {
      const made = await service.createLocation(text);
      if (!locations.some((l) => l.id === made.id)) {
        locations = [...locations, made];
      }
      onpick(made.id);
    } catch (thrown) {
      fault = thrown;
    }
  };
</script>

<div class={['flex flex-col gap-2', className]}>
  <Field
    label={t('materials.spool.location')}
    control={Combobox}
    controlProps={{
      items,
      value: value === undefined ? undefined : String(value),
      label: t('materials.spool.location'),
      placeholder: t('materials.location.placeholder'),
      noneLabel: t('materials.location.none'),
      createLabel: (text: string) =>
        t('materials.location.create', { name: text }),
      onCreate: create,
      onPick: (next: string | undefined) => {
        fault = undefined;
        onpick(next);
      },
    }}
  />
  <FaultNote {fault} />
</div>
