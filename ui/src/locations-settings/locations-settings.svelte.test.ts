import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import { type Answer, type Stub, stubHost } from '../test/host';

const NS = 'life.honex.gitlab.aux.spoolsync.spool-sync-prusa-connect';
const LOCATIONS = `/api/v1/modules/${NS}/locations`;

const shelf = { id: '1', name: 'Shelf', spools: 3 };
const box = { id: '2', name: 'Box', spools: 0 };

const draw = async (
  routed: Record<string, readonly Answer[]>,
  options: { mayOperate?: boolean } = {},
): Promise<Stub> => {
  const stub = stubHost([{ status: 200, body: {} }], { ...options, routed });
  void setup(stub.host);
  await render(stub.registered.settings.locations!, {});

  return stub;
};

const listed = (...locations: unknown[]): Answer => ({
  status: 200,
  body: { locations },
});

describe('locations settings', () => {
  it('lists the locations with their spools', async () => {
    await draw({ [LOCATIONS]: [listed(box, shelf)] });
    await expect.element(page.getByText('3 spools')).toBeVisible();
    await expect.element(page.getByText('0 spools')).toBeVisible();
  });

  it('renames a location in its row', async () => {
    const stub = await draw({
      [LOCATIONS]: [listed(shelf)],
      [`${LOCATIONS}/1`]: [
        { status: 200, body: { ...shelf, name: 'Top shelf' } },
      ],
    });
    await page.getByRole('button', { name: 'Rename Shelf' }).click();
    await page.getByRole('textbox', { name: 'Rename Shelf' }).fill('Top shelf');
    await page.getByRole('button', { name: 'Save' }).click();

    await expect
      .poll(() => stub.calls.find((c) => c.method === 'PATCH')?.body)
      .toBe(JSON.stringify({ name: 'Top shelf' }));
  });

  it('says how many spools a delete leaves with no location, then deletes', async () => {
    const stub = await draw({
      [LOCATIONS]: [listed(shelf), listed()],
      [`${LOCATIONS}/1`]: [{ status: 204, body: null }],
    });
    await page.getByRole('button', { name: 'Delete Shelf' }).click();
    const ask = page.getByRole('alertdialog');
    await expect
      .element(ask.getByText('3 spools will be left with no location.'))
      .toBeVisible();
    expect(stub.calls.some((c) => c.method === 'DELETE')).toBe(false);

    await ask.getByRole('button', { name: 'Delete' }).click();
    await expect
      .poll(() => stub.calls.find((c) => c.method === 'DELETE')?.url)
      .toContain('/locations/1');
  });

  it('shows a person who may only look the list without controls', async () => {
    await draw({ [LOCATIONS]: [listed(shelf)] }, { mayOperate: false });
    await expect
      .element(page.getByText('Shelf', { exact: true }))
      .toBeVisible();
    expect(page.getByRole('button', { name: 'Rename Shelf' }).query()).toBe(
      null,
    );
    expect(page.getByRole('textbox', { name: 'New location' }).query()).toBe(
      null,
    );
  });
});
