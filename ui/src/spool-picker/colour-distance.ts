// How far apart two colours look, for putting the spools nearest a tool's
// colour first. The distance is in OKLab, where equal steps look about equally
// different, so a dark blue is nearer navy than it is black. Alpha is ignored:
// a filament's colour is its colour.

import { cssColour } from '../css-colour';

type Lab = readonly [number, number, number];

const linear = (c: number): number => {
  const v = c / 255;

  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
};

/** #rrggbb or #rrggbbaa in OKLab, or undefined for anything else. */
export const oklab = (colour: string | undefined): Lab | undefined => {
  const hex = cssColour(colour);
  if (hex === undefined) return undefined;

  const r = linear(parseInt(hex.slice(1, 3), 16));
  const g = linear(parseInt(hex.slice(3, 5), 16));
  const b = linear(parseInt(hex.slice(5, 7), 16));

  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);

  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
};

/**
 * The items nearest `target` in colour first. An item with no colour goes
 * last, and items the same distance away keep their order. Without a target
 * the items come back as they were.
 */
export const byColour = <T>(
  items: readonly T[],
  target: string | undefined,
  colourOf: (item: T) => string | undefined,
): T[] => {
  const to = oklab(target);
  if (to === undefined) return [...items];

  const distance = (item: T): number => {
    const from = oklab(colourOf(item));

    return from === undefined
      ? Number.POSITIVE_INFINITY
      : Math.hypot(from[0] - to[0], from[1] - to[1], from[2] - to[2]);
  };

  return items
    .map((item, index) => ({ item, index, d: distance(item) }))
    .sort((a, b) => a.d - b.d || a.index - b.index)
    .map((x) => x.item);
};
