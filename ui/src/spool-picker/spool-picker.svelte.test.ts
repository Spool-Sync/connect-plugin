import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { Spool } from '../api';
import setup from '../index';
import { stubHost } from '../test/host';
import SpoolPicker from './spool-picker.svelte';

const spool = (id: string, extra: Partial<Spool> = {}): Spool =>
  ({
    id,
    uuid: `uuid-${id}`,
    source: 'manual',
    material_uuid: 'a',
    material_source: 'catalogue',
    material_name: 'Galaxy Black',
    brand_name: 'Prusament',
    class: 'FFF',
    type: 'PLA',
    color_rgba: '#3d3e3dff',
    full_weight_g: 1000,
    remaining_weight_g: 640.4,
    location: '',
    notes: '',
    created_by: 'user:demo',
    created_at: '2026-10-28T10:00:00Z',
    updated_at: '2026-10-28T10:00:00Z',
    archived_at: null,
    tool: null,
    ...extra,
  }) as Spool;

const draw = async (
  spools: readonly Spool[],
  selected?: string,
  material?: string,
) => {
  const onselect = vi.fn<(id: string | undefined) => void>();
  const stub = stubHost([{ status: 200, body: {} }]);
  void setup(stub.host);
  await render(SpoolPicker, { spools, selected, onselect, material });

  return { onselect };
};

describe('the spool picker', () => {
  it('offers only the material the printer reports, with no type to pick', async () => {
    await draw(
      [
        spool('1'),
        spool('2', { material_name: 'Woodfill', type: 'WOOD' }),
        spool('3', { material_name: 'PETG Clear', type: 'PETG' }),
      ],
      undefined,
      'PLA',
    );

    await expect
      .element(page.getByRole('button', { name: /Galaxy Black/ }))
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: /Woodfill/ }))
      .toBeVisible();
    expect(page.getByRole('button', { name: /PETG Clear/ }).query()).toBe(null);
    expect(page.getByRole('group', { name: 'Material types' }).query()).toBe(null);
    await expect
      .element(page.getByText('the printer reports PLA on this tool', { exact: false }))
      .toBeVisible();
  });

  it('says no spool of the reported material is there', async () => {
    await draw([spool('3', { type: 'PETG' })], undefined, 'ASA');
    await expect
      .element(page.getByText('No ASA spools available.', { exact: false }))
      .toBeVisible();
  });

  it('lists the spools with their weight, location and tool', async () => {
    await draw([
      spool('1', { location: 'Shelf A' }),
      spool('2', {
        brand_name: 'Acme',
        material_name: 'PETG Clear',
        type: 'PETG',
        color_rgba: '',
        tool: { device_id: 'core', tool_id: '0', gone: false },
      }),
    ]);

    const galaxy = page.getByRole('button', { name: /Prusament Galaxy Black/ });
    await expect.element(galaxy).toBeVisible();
    expect(galaxy.element().textContent).toContain('640');
    expect(galaxy.element().textContent).toContain('Shelf A');

    const clear = page.getByRole('button', { name: /Acme PETG Clear/ });
    await expect.element(clear).toBeVisible();
    expect(clear.element().textContent).toContain('On Tool 1');
  });

  it('narrows by the search across brand, name, type and location', async () => {
    await draw([
      spool('1', { location: 'Shelf A' }),
      spool('2', { brand_name: 'Acme', material_name: 'PETG Clear', type: 'PETG' }),
    ]);

    await page.getByRole('searchbox').fill('shelf');
    await expect.element(page.getByRole('button', { name: /Galaxy/ })).toBeVisible();
    expect(page.getByRole('button', { name: /PETG Clear/ }).query()).toBeNull();

    await page.getByRole('searchbox').fill('nothing like this');
    await expect
      .element(page.getByText('No spool matches your filters'))
      .toBeVisible();
  });

  it('chooses a manufacturer, and offers only its material types', async () => {
    await draw([
      spool('1'),
      spool('2', { brand_name: 'Acme', material_name: 'PETG Clear', type: 'PETG' }),
    ]);

    await page.getByRole('button', { name: 'Acme', exact: true }).click();
    await expect
      .element(page.getByRole('button', { name: /PETG Clear/ }))
      .toBeVisible();
    expect(page.getByRole('button', { name: /Galaxy/ }).query()).toBeNull();
    expect(page.getByRole('button', { name: 'PLA', exact: true }).query()).toBeNull();

    await page.getByRole('button', { name: 'Acme', exact: true }).click();
    await expect.element(page.getByRole('button', { name: /Galaxy/ })).toBeVisible();
  });

  it('reports the spool picked', async () => {
    const { onselect } = await draw([spool('1'), spool('2', { material_name: 'Other' })]);

    const first = page.getByRole('button', { name: /Galaxy/ });
    await first.click();
    expect(onselect).toHaveBeenLastCalledWith('1');

    // The host owns the choice: nothing is marked until it passes one back.
    await expect.element(first).toHaveAttribute('aria-pressed', 'false');
  });

  it('marks the chosen spool and deselects it on a second press', async () => {
    const { onselect } = await draw([spool('1'), spool('2', { material_name: 'Other' })], '1');

    const chosen = page.getByRole('button', { name: /Galaxy/ });
    await expect.element(chosen).toHaveAttribute('aria-pressed', 'true');
    await chosen.click();
    expect(onselect).toHaveBeenLastCalledWith(undefined);
  });

  it('puts a spool on a tool after the others', async () => {
    await draw([
      spool('1', { tool: { device_id: 'core', tool_id: '0', gone: false }, material_name: 'On tool' }),
      spool('2', { material_name: 'Free' }),
    ]);

    const names = [...document.querySelectorAll('[data-part="spool-row"]')].map((b) =>
      b.textContent?.trim(),
    );
    expect(names[0]).toContain('Free');
    expect(names[1]).toContain('On tool');
  });

  it('says there are no spools when there are none', async () => {
    await draw([]);
    await expect.element(page.getByText('No spools available')).toBeVisible();
  });
});
