import { formatNumber } from '@prusa/connect-ui/i18n';

/** Grams as the kilograms a person reads, one decimal at most. */
export const kilograms = (grams: number): string =>
  formatNumber(grams / 1000, { maximumFractionDigits: 1 });

/**
 * A weight as typed, or undefined for an empty field. The field is text
 * with a decimal keyboard, because a number field in Safari under a
 * comma-decimal region answers '' for "742,5"; the comma is read here.
 */
export const typedWeight = (typed: string): number | undefined => {
  const value = typed.trim().replace(',', '.');

  return value === '' ? undefined : Number(value);
};
