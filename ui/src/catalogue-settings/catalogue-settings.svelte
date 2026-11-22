<!--
  The catalogue the module holds, and the button that loads a newer one.

  A settings pane takes no props. The host draws it on the Extensions screen
  with no heading, so the pane names itself, and that screen's ground is the
  pane's own colour, so the pane is edged. Whoever opens the pane while an
  import runs (somebody else may have started it) sees it finish: the poll
  starts on mount and ends with the host's signal or when the pane goes.
-->
<script lang="ts">
  import { formatDateTime } from '@prusa/connect-ui/i18n';
  import {
    Button,
    FaultAlert,
    ListingSkeleton,
    SettingsPane,
    SettingsRow,
  } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { heldCatalogue } from '../catalogue.svelte';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { heldHost } from '../host';
  import { heldList } from '../list.svelte';

  const catalogue = heldCatalogue();
  const list = heldList();
  const host = heldHost();
  const t = host.t;

  const status = $derived(catalogue.status);
  const running = $derived(status?.import.phase === 'running');
  // What the last import brought in. SpoolSync keeps no version or hash of the
  // OpenPrintTag database, so the pane shows the counts and the time instead.
  const counts = $derived(
    status
      ? ([
          ['imported', status.import.imported ?? 0],
          ['updated', status.import.updated ?? 0],
          ['skipped', status.import.skipped ?? 0],
        ] as const)
      : [],
  );
  const unchanged = $derived(
    (status?.import.imported ?? 0) === 0 && (status?.import.updated ?? 0) === 0,
  );

  // The step a failed import stopped at; the error itself is in the module's log.
  const failedText = {
    download: 'materials.settings.failedDownload',
    catalogue: 'materials.settings.failedCatalogue',
    store: 'materials.settings.failedStore',
  } as const;

  // The server keeps the last import's outcome until it restarts, so it is
  // announced only to a pane that saw the import run.
  let seen = $state(false);
  $effect(() => {
    if (running) seen = true;
  });

  // Ends with the host's signal or when this pane is destroyed.
  const ended = new AbortController();
  const signal = AbortSignal.any([ended.signal, host.signal]);

  onMount(() => {
    void catalogue.read().then(() => catalogue.poll(signal));

    return () => ended.abort();
  });

  const download = async (): Promise<void> => {
    if (await catalogue.download()) {
      seen = true;
      catalogue.poll(signal);
    }
  };
</script>

{#snippet actions()}
  <Button disabled={running || catalogue.busy} onclick={download}>
    {t('materials.settings.download')}
  </Button>
{/snippet}

<SettingsPane
  title={t('materials.settings.title')}
  edged
  actions={list.mayWrite ? actions : undefined}
>
  <div
    class="flex max-w-3xl flex-col gap-section-gap p-panel-padding-mobile @lg/pane:p-panel-padding"
  >
    {#if !catalogue.loaded}
      <ListingSkeleton label={t('materials.settings.reading')} rows={3} />
    {:else if status?.loaded}
      <div class="flex flex-col gap-form-note">
        {#each counts as [name, value] (name)}
          <SettingsRow label={t(`materials.settings.${name}`)}>
            {#snippet control()}<span data-part={name}>{value}</span>{/snippet}
          </SettingsRow>
        {/each}
        {#if status.import.finished}
          <SettingsRow label={t('materials.settings.finished')}>
            {#snippet control()}{formatDateTime(status.import.finished ?? "")}{/snippet}
          </SettingsRow>
        {/if}
      </div>
    {:else}
      <p class="text-body">{t('materials.settings.none')}</p>
    {/if}
    {#if status?.import.phase === 'failed'}
      <FaultAlert
        fault={{
          code: undefined,
          detail: undefined,
          title: t(failedText[(status.import.reason ?? 'download') as keyof typeof failedText] ?? failedText.download),
        }}
      />
    {:else if seen && status?.import.phase === 'idle' && status.import.finished}
      <p data-part="result" class="text-body text-muted-foreground">
        {unchanged
          ? t('materials.settings.unchanged')
          : t('materials.settings.updated')}
      </p>
    {/if}
    {#if running}
      <p data-part="running" class="text-body text-muted-foreground">
        {t('materials.settings.running')}
      </p>
    {/if}
    <FaultNote fault={catalogue.fault} />
  </div>
</SettingsPane>
