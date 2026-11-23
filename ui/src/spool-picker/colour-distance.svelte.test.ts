import { describe, expect, it } from 'vitest';

import { byColour, oklab } from './colour-distance';

describe('ordering by colour', () => {
  it('reads #rrggbb and #rrggbbaa, and nothing else', () => {
    expect(oklab('#ffffff')?.[0]).toBeCloseTo(1, 3);
    expect(oklab('#000000ff')?.[0]).toBeCloseTo(0, 3);
    expect(oklab('blue')).toBe(undefined);
    expect(oklab(undefined)).toBe(undefined);
  });

  it('puts the nearest colour first and a spool with none last', () => {
    const spools = [
      { id: 'black', c: '#000000' },
      { id: 'none', c: '' },
      { id: 'navy', c: '#1a237e' },
      { id: 'red', c: '#d32f2f' },
      { id: 'sky', c: '#1e88e5' },
    ];
    // In OKLab a lighter blue is nearer than navy, and black, far in
    // lightness, is further than red.
    const order = byColour(spools, '#1565c0ff', (s) => s.c).map((s) => s.id);
    expect(order).toEqual(['sky', 'navy', 'red', 'black', 'none']);
  });

  it('keeps the order without a colour to match', () => {
    const spools = [{ c: '#ff0000' }, { c: '#00ff00' }];
    expect(byColour(spools, undefined, (s) => s.c)).toEqual(spools);
  });
});
