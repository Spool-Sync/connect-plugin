import { describe, expect, it } from 'vitest';

import type { Detail } from '../api';
import { SpoolDraftForm } from './draft.svelte';

const detail: Detail = {
  uuid: 'm1',
  name: 'Galaxy Black',
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
  packages: [],
};

const chosen = (): SpoolDraftForm => {
  const draft = new SpoolDraftForm();
  draft.choose(detail);

  return draft;
};

describe('the spool draft', () => {
  it('moves to step 2 with the material chosen', () => {
    const draft = chosen();
    expect(draft.step).toBe(2);
    expect(draft.material?.uuid).toBe('m1');
  });

  it('gives a spool with nothing typed left the full weight less the empty one', () => {
    const draft = chosen();
    draft.fullWeightG = 1250;
    draft.emptyWeightG = 250;
    expect(draft.valid).toBe(true);
    expect(draft.body()).toMatchObject({
      material_uuid: 'm1',
      remaining_weight_g: 1000,
      quantity: 1,
    });
    expect(draft.body()).not.toHaveProperty('condition');
  });

  it('asks for what is left without a net weight, and no more than the net', () => {
    const draft = chosen();
    expect(draft.valid).toBe(false);
    draft.remainingG = 400;
    expect(draft.valid).toBe(true);
    // Full and empty weights make a net of 800 g: what is left may not pass it.
    draft.fullWeightG = 1000;
    draft.emptyWeightG = 200;
    expect(draft.valid).toBe(true);
    expect(draft.body().remaining_weight_g).toBe(400);
    draft.remainingG = 801;
    expect(draft.valid).toBe(false);
  });

  it('takes a quantity from 1 to 100', () => {
    const draft = chosen();
    draft.fullWeightG = 1000;
    draft.emptyWeightG = 200;
    for (const [quantity, valid] of [
      [0, false],
      [1, true],
      [100, true],
      [101, false],
      [2.5, false],
    ] as const) {
      draft.quantity = quantity;
      expect(draft.valid, `quantity ${String(quantity)}`).toBe(valid);
    }
  });

});
