import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import { heldList } from '../list.svelte';
import { type Answer, type Stub, stubHost } from '../test/host';

const NS = 'life.honex.gitlab.aux.spoolsync.spool-sync-prusa-connect';
const CATALOGUE = `/api/v1/modules/${NS}/catalogue`;
const DOWNLOAD = `${CATALOGUE}/download`;

const FIN = '2026-10-02T10:00:00Z';
const idle = { phase: 'idle' };
const meta = (extra: Record<string, unknown> = {}) => ({
  data_hash: 'abcdef0123456789abcdef',
  last_modified: '2026-10-01',
  brands: 12,
  materials: 345,
  packages: 678,
  containers: 9,
  skipped: 0,
  imported_at: '2026-10-02T10:00:00Z',
  ...extra,
});
const loaded = (extra = {}, imp: Record<string, unknown> = idle) => ({
  loaded: true,
  meta: meta(extra),
  import: imp,
});

const drawn = async (
  read: Answer[],
  download: Answer[] = [],
  options: { mayOperate?: boolean } = {},
): Promise<{ stub: Stub; view: Awaited<ReturnType<typeof render>> }> => {
  const stub = stubHost([{ status: 200, body: {} }], {
    ...options,
    routed: { [CATALOGUE]: read, [DOWNLOAD]: download },
  });
  void setup(stub.host);
  const view = await render(stub.registered.settings.catalogue!, {});

  return { stub, view };
};

const draw = async (...args: Parameters<typeof drawn>): Promise<Stub> =>
  (await drawn(...args)).stub;

const ok = (body: unknown): Answer => ({ status: 200, body });

describe('catalogue settings', () => {
  it('says no catalogue is loaded', async () => {
    await draw([ok({ loaded: false, import: idle })]);
    await expect.element(page.getByText('No catalogue loaded')).toBeVisible();
  });

  it('shows the version, the counts and the short hash', async () => {
    await draw([ok(loaded({ skipped: 3 }))]);
    await expect.element(page.getByText('2026-10-01')).toBeVisible();
    await expect.element(page.getByText('abcdef012345')).toBeVisible();
    await expect
      .element(page.getByTestId('materials'))
      .toHaveTextContent('345');
    await expect.element(page.getByTestId('skipped')).toHaveTextContent('3');
  });

  it('leaves the skipped count away when it is 0', async () => {
    await draw([ok(loaded())]);
    await expect.element(page.getByTestId('materials')).toBeVisible();
    expect(page.getByTestId('skipped').elements()).toHaveLength(0);
  });

  it('downloads, polls while it runs, then shows the new counts', async () => {
    const stub = await draw(
      [
        ok({ loaded: false, import: idle }),
        ok({ loaded: false, import: { phase: 'running' } }),
        ok(
          loaded(
            { materials: 777 },
            { phase: 'idle', finished: FIN, unchanged: false },
          ),
        ),
      ],
      [ok({ loaded: false, import: { phase: 'running' } })],
    );
    await page.getByRole('button', { name: 'Download from GitHub' }).click();
    await expect.element(page.getByTestId('running')).toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Download from GitHub' }))
      .toBeDisabled();
    await expect
      .element(page.getByTestId('materials'), { timeout: 8000 })
      .toHaveTextContent('777');
    await expect.element(page.getByText('Catalogue updated')).toBeVisible();
    expect(stub.calls.some((c) => c.url.endsWith('/catalogue/download'))).toBe(
      true,
    );
  });

  it('says already up to date after a download that found nothing new', async () => {
    await draw(
      [
        ok(loaded()),
        ok(loaded({}, { phase: 'idle', finished: FIN, unchanged: true })),
      ],
      [ok(loaded({}, { phase: 'running' }))],
    );
    await page.getByRole('button', { name: 'Download from GitHub' }).click();
    await expect
      .element(page.getByText('Already up to date'), { timeout: 8000 })
      .toBeVisible();
  });

  it('claims no result on a plain visit', async () => {
    await draw([
      ok(loaded({}, { phase: 'idle', finished: FIN, unchanged: false })),
    ]);
    await expect.element(page.getByTestId('materials')).toBeVisible();
    expect(page.getByTestId('result').elements()).toHaveLength(0);
  });

  it('shows the step an import failed at', async () => {
    await draw([ok(loaded({}, { phase: 'failed', reason: 'catalogue' }))]);
    await expect
      .element(page.getByText(/downloaded a file that is not a catalogue/))
      .toBeVisible();
  });

  it('polls on mount when an import already runs', async () => {
    await draw([
      ok({ loaded: false, import: { phase: 'running' } }),
      ok(loaded({ materials: 55 })),
    ]);
    await expect
      .element(page.getByTestId('materials'), { timeout: 8000 })
      .toHaveTextContent('55');
  });

  it('follows the import somebody else started when its own is refused', async () => {
    await draw(
      [
        ok({ loaded: false, import: idle }),
        ok({ loaded: false, import: { phase: 'running' } }),
      ],
      [
        {
          status: 409,
          contentType: 'application/problem+json',
          body: {
            type: 'about:blank',
            title: 'Conflict',
            status: 409,
            code: 'materials/IMPORT_RUNNING',
            detail: 'an import is already running',
          },
        },
      ],
    );
    await page.getByRole('button', { name: 'Download from GitHub' }).click();
    await expect.element(page.getByTestId('running')).toBeVisible();
    expect(page.getByRole('alert').query()).toBe(null);
  });

  it('shows viewers no button', async () => {
    await draw([ok(loaded())], [], { mayOperate: false });
    await expect.element(page.getByTestId('materials')).toBeVisible();
    // The access answer is in, so the missing button is a refusal.
    await vi.waitFor(() => {
      expect(heldList().mayOnlyRead).toBe(true);
    });
    expect(
      page.getByRole('button', { name: 'Download from GitHub' }).elements(),
    ).toHaveLength(0);
  });

  it('stops polling when the pane is destroyed', async () => {
    const { stub, view } = await drawn([
      ok({ loaded: false, import: { phase: 'running' } }),
    ]);
    await expect.element(page.getByTestId('running')).toBeVisible();
    await view.unmount();
    const reads = (): number =>
      stub.calls.filter((c) => c.url.endsWith('/catalogue')).length;
    const before = reads();
    await new Promise((resolve) => setTimeout(resolve, 2500));
    expect(reads()).toBe(before);
  });
});
