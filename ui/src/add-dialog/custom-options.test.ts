import { describe, expect, it } from 'vitest';

import { PALETTE, PRESETS } from './custom-options';

describe('the presets a custom material offers', () => {
  it('names each filament type once, and gives it a density in g/cm³', () => {
    const types = PRESETS.map((p) => p.type);
    expect(new Set(types).size).toBe(types.length);
    for (const preset of PRESETS) {
      const density = preset.values.density;
      expect(density, preset.type).toBeGreaterThan(0.9);
      expect(density, preset.type).toBeLessThan(2);
    }
  });
});

describe('the palette a custom material offers', () => {
  it('lists colours as #rrggbb, so a swatch can paint them', () => {
    for (const hex of PALETTE) {
      expect(hex).toMatch(/^#[0-9a-f]{6}$/u);
    }
  });
});
