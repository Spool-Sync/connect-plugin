import { describe, expect, it } from 'vitest';

import { requiredMaterial, sameMaterial } from './material-match';

describe('the material a tool takes', () => {
  it('is the known type a printer reports, and nothing for a name of its own or none', () => {
    expect(requiredMaterial('PLA')).toBe('PLA');
    expect(requiredMaterial('petg')).toBe('petg');
    expect(requiredMaterial('My Silk Blend')).toBe(undefined);
    expect(requiredMaterial('')).toBe(undefined);
    expect(requiredMaterial(undefined)).toBe(undefined);
  });

  it('matches in any case and across an alias group, as the module does', () => {
    expect(sameMaterial('PLA', 'pla')).toBe(true);
    expect(sameMaterial('PLA', 'WOOD')).toBe(true);
    expect(sameMaterial('PC', 'PCCF')).toBe(true);
    expect(sameMaterial('FLEX', 'TPU')).toBe(true);
    expect(sameMaterial('PLA', 'PETG')).toBe(false);
    expect(sameMaterial('PLA', '')).toBe(false);
  });
});
