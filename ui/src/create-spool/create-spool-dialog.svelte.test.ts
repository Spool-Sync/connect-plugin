import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import { type Answer, type Stub, stubHost } from '../test/host';
import CreateSpoolDialog from './create-spool-dialog.svelte';

const NS = 'life.honex.gitlab.aux.spoolsync.spool-sync-prusa-connect';
const M = `/api/v1/modules/${NS}`;

const row = (uuid: string, name: string, extra: Record<string, unknown> = {}) => ({
  uuid,
  name,
  brand_name: 'Prusament',
  class: 'FFF',
  type: 'PLA',
  color_rgba: '#3d3e3dff',
  is_favourite: false,
  is_custom: false,
  spools: 0,
  assigned_spools: 0,
  remaining_weight_g: 0,
  free_g: 0,
  ...extra,
});

const started = (routed: Record<string, readonly Answer[]> = {}): Stub => {
  const stub = stubHost([{ status: 200, body: {} }], {
    routed: {
      [`${M}/catalogue/brands`]: [
        { status: 200, body: { brands: [{ uuid: 'b1', slug: 'prusament', name: 'Prusament' }] } },
      ],
      [`${M}/types`]: [{ status: 200, body: { types: ['ASA', 'PLA'] } }],
      [`${M}/locations`]: [
        { status: 200, body: { locations: [{ id: '7', name: 'Shelf A', spools: 0 }] } },
      ],
      [`${M}/materials`]: [
        {
          status: 200,
          body: {
            rows: [row('a', 'Galaxy Black'), row('b', 'Jet Black')],
            total: 2,
            stock_g: 0,
            stock_spools: 0,
          },
        },
      ],
      [`${M}/spools`]: [{ status: 201, body: { added: 1, ids: ['9'] } }],
      [`${M}/custom-materials`]: [{ status: 201, body: { uuid: 'c1' } }],
      ...routed,
    },
  });
  void setup(stub.host);

  return stub;
};

const posted = (stub: Stub, path: string): unknown[] =>
  stub.calls
    .filter((c) => c.method === 'POST' && c.url.endsWith(path))
    .map((c) => (c.body === undefined ? undefined : (JSON.parse(c.body) as unknown)));

const draw = async (oncreated = vi.fn(), onclose = vi.fn()) => {
  await render(CreateSpoolDialog, { open: true, onclose, oncreated });

  return { oncreated, onclose };
};

describe('the create spool dialog', () => {
  it('keeps the material type off until a manufacturer is named', async () => {
    started();
    await draw();

    await expect
      .element(page.getByRole('combobox', { name: 'Material type' }))
      .toBeDisabled();
    await page.getByRole('combobox', { name: 'Manufacturer' }).fill('Prusament');
    await page.getByRole('option', { name: 'Prusament' }).click();
    await expect
      .element(page.getByRole('combobox', { name: 'Material type' }))
      .toBeEnabled();
  });

  it('shows the colours of the manufacturer and type, and posts the spool made of one', async () => {
    const stub = started();
    const { oncreated, onclose } = await draw();

    await page.getByRole('combobox', { name: 'Manufacturer' }).fill('Prusament');
    await page.getByRole('option', { name: 'Prusament' }).click();
    await page.getByRole('combobox', { name: 'Material type' }).click();
    await page.getByRole('option', { name: 'PLA', exact: true }).click();

    const create = page.getByRole('button', { name: 'Create', exact: true });
    await expect.element(create).toBeDisabled();
    await page.getByRole('button', { name: 'Jet Black' }).click();
    await page.getByLabelText('Initial weight (g) *').fill('1000');
    await page.getByLabelText('Current weight (g)').fill('640');
    await create.click();

    await expect.poll(() => posted(stub, '/spools')).toEqual([
      {
        material_uuid: 'b',
        full_weight_g: 1000,
        remaining_weight_g: 640,
        notes: '',
        quantity: 1,
      },
    ]);
    await expect.poll(() => oncreated.mock.calls).toEqual([['9']]);
    expect(onclose).toHaveBeenCalled();
  });

  it('keeps the current weight equal to the initial one until it is typed', async () => {
    started();
    await draw();

    await page.getByLabelText('Initial weight (g) *').fill('750');
    await expect.element(page.getByLabelText('Current weight (g)')).toHaveValue('750');
  });

  it('adds a colour the catalogue lacks as a custom material, and picks it', async () => {
    const stub = started();
    await draw();

    await page.getByRole('combobox', { name: 'Manufacturer' }).fill('Acme');
    await page.getByRole('option', { name: 'Create “Acme”' }).click();
    await page.getByRole('combobox', { name: 'Material type' }).click();
    await page.getByRole('option', { name: 'ASA', exact: true }).click();

    await page.getByRole('button', { name: 'Add colour' }).click();
    await page.getByLabelText('Colour name').fill('Sunset');
    await page.getByRole('button', { name: 'Add colour' }).click();

    await expect
      .element(page.getByRole('button', { name: 'Sunset' }))
      .toHaveAttribute('aria-pressed', 'true');
    const made = stub.calls.find((c) => c.url.endsWith('/custom-materials'));
    expect(JSON.parse(made!.body!)).toMatchObject({
      name: 'Sunset',
      brand_name: 'Acme',
      class: 'FFF',
      type: 'ASA',
      color_rgba: '#808080ff',
      diameter_um: 1750,
    });
  });
});
