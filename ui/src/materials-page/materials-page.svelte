<!--
  The SpoolSync page: the stock at a glance, then the materials in stock,
  searchable.

  A page takes no props: what it draws comes from the list the module's setup
  built. The section is the container every child reads its width from, so the
  table turns into cards when the page is narrow (frontend-css.md).

  The skeleton stands in rows of the height the table's rows will have, so the
  header and the list keep their places when the data arrives. A person who may
  not write sees the list and no control the module would refuse.
-->
<script lang="ts">
  import {
    Button,
    Card,
    CardContent,
    EmptyState,
    ListingNoMatch,
    ListingSkeleton,
    PageHeader,
    Table,
    TableBody,
    TableHead,
    TableHeader,
    TableRow,
  } from '@prusa/connect-ui/primitives';
  import PlusIcon from '@lucide/svelte/icons/plus';
  import { onMount } from 'svelte';

  import AddDialog from '../add-dialog/add-dialog.svelte';
  import CreateSpoolDialog from '../create-spool/create-spool-dialog.svelte';
  import { materialsOn, type Overview } from '../api';
  import { heldHost } from '../host';
  import { follow } from '../live';
  import { heldList } from '../list.svelte';
  import { kept } from './remembered';
  import FaultNote from './fault-note.svelte';
  import ListFilters from './list-filters.svelte';
  import MaterialRow from './material-row.svelte';
  import OverviewPanel from './overview.svelte';

  const list = heldList();
  const host = heldHost();
  const t = host.t;

  /** The add dialog, open on a row's material, from the row's + . */
  let adding = $state<{ material?: string | undefined } | undefined>(undefined);
  /** The Create spool dialog, from the header and the empty state. */
  let creatingSpool = $state(false);

  const narrowed = $derived(Object.keys(list.query).length > 0);

  const service = materialsOn(host);

  /** The dashboard above the list, undefined until it is read. */
  let overview = $state<Overview | undefined>(undefined);
  const EMPTY: Overview = {
    spools: 0,
    grams: 0,
    materials: 0,
    by_material: [],
    by_type: [],
    usage: [],
  };
  /** Reads the overview again. One that fails keeps what was shown. */
  const reread = (): void => {
    service
      .overview()
      .then((fresh) => (overview = { ...EMPTY, ...fresh }))
      .catch(() => (overview ??= EMPTY));
  };
  /** After a write: the list and the overview both read again. */
  const refresh = (): void => {
    void list.read();
    reread();
  };
  /** A material tile narrows the list to its type, and a second press lets go. */
  const pick = (material: string): void => {
    void list.narrow({ type: list.query.type === material ? undefined : material });
  };
  /** The filters as the module keeps them, once they are read. */
  let saved: string | undefined;

  // The list opens with the filters the person kept, unless it already has
  // some from earlier in this visit. A read that fails opens it unfiltered.
  onMount(() => {
    const stream = new AbortController();
    const signal = AbortSignal.any([host.signal, stream.signal]);
    reread();
    const initial = (async () => {
      const fresh = !list.loaded;
      const recalled = fresh
        ? await service
            .filters()
            .then(kept)
            .catch(() => ({}))
        : {};
      const wanted = { ...recalled, ...list.query };
      saved = JSON.stringify(kept(wanted));
      await list.narrow(wanted);
    })();
    let timer: ReturnType<typeof setTimeout> | undefined;
    follow(host.client, signal, ['tool'], () => {
      clearTimeout(timer);
      timer = setTimeout(() => {
        void initial.then(() => {
          if (signal.aborted) return;
          void list.narrow({});
          reread();
        });
      }, 300);
    });
    return () => {
      clearTimeout(timer);
      stream.abort();
    };
  });

  // Each change is kept for the next visit. A person who may only look is
  // refused the write, which costs them the memory and nothing else.
  $effect(() => {
    const now = JSON.stringify(kept(list.query));
    if (saved === undefined || now === saved) return;
    saved = now;
    service.keepFilters(kept(list.query)).catch(() => undefined);
  });
</script>

<section class="@container flex flex-col gap-4">
  <!-- Until the core answers, the actions are drawn invisible so the header keeps its height. -->
  <PageHeader title={t('materials.page.title')}>
    {#snippet actions()}
      {#if list.mayWrite || !list.mayOnlyRead}
        {@const hold = !list.mayWrite}
        <Button
          size="md"
          class={hold ? 'invisible' : undefined}
          aria-hidden={hold ? 'true' : undefined}
          tabindex={hold ? -1 : undefined}
          disabled={hold}
          onclick={() => (creatingSpool = true)}
        >
          <PlusIcon aria-hidden="true" />
          {t('materials.page.addSpool')}
        </Button>
      {/if}
    {/snippet}
  </PageHeader>

  <OverviewPanel part="top" {overview} />

  {#if list.mayOnlyRead}
    <p data-part="viewer" class="text-sm text-muted-foreground">
      {t('materials.page.viewer')}
    </p>
  {/if}

  <FaultNote fault={list.fault} />

  <Card>
    <CardContent class="flex flex-col gap-4">
      <ListFilters />

      <div data-part="listing">
        {#if !list.loaded}
          <!-- A table row is taller than a bar, so nine bars hold the place
               of the header and the first five rows. -->
          <ListingSkeleton label={t('materials.page.reading')} rows={9} art />
        {:else if list.rows.length === 0}
          {#if narrowed}
            <ListingNoMatch
              headingLevel="h2"
              heading={t('materials.page.empty.filteredHeading')}
              description={t('materials.page.empty.filtered')}
              onClear={() => void list.clear()}
            />
          {:else}
            <EmptyState
              headingLevel="h2"
              heading={t('materials.page.empty.heading')}
              description={t('materials.page.empty.description')}
            >
              {#snippet action()}
                {#if list.mayWrite}
                  <Button size="sm" onclick={() => (creatingSpool = true)}>
                    {t('materials.page.addSpool')}
                  </Button>
                {/if}
              {/snippet}
            </EmptyState>
          {/if}
        {:else}
          <Table class="@max-md:block">
            <TableHeader class="@max-md:hidden">
              <TableRow>
                <TableHead>{t('materials.column.colour')}</TableHead>
                <TableHead>{t('materials.column.name')}</TableHead>
                <TableHead>{t('materials.column.brand')}</TableHead>
                <TableHead>{t('materials.column.type')}</TableHead>
                <TableHead>{t('materials.column.spools')}</TableHead>
                <TableHead>{t('materials.column.weight')}</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody class="@max-md:block @max-md:space-y-3">
              {#each list.rows as row (row.uuid)}
                <MaterialRow
                  {row}
                  onadd={(material: string) => (adding = { material })}
                />
              {/each}
            </TableBody>
          </Table>

          {#if list.next !== undefined}
            <Button
              variant="secondary"
              class="mt-4"
              disabled={list.busy}
              onclick={() => void list.more()}
            >
              {t('materials.page.more')}
            </Button>
          {/if}
        {/if}
      </div>
    </CardContent>
  </Card>
  <OverviewPanel part="bottom" {overview} selected={list.query.type} onpick={pick} />
</section>

<AddDialog
  open={adding !== undefined}
  material={adding?.material}
  onclose={() => (adding = undefined)}
  onadded={refresh}
/>

<CreateSpoolDialog
  open={creatingSpool}
  onclose={() => (creatingSpool = false)}
  oncreated={refresh}
/>
