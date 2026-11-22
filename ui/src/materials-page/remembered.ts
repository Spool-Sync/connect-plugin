// The list's filters and order, kept per person in the module's database,
// so the page opens with no filter the first time and with the person's own
// after that, on any browser they sign in from.
//
// The module keeps whatever object the page sends, so the page reads back
// only what it keeps and nothing else.

import type { ListQuery } from '../api';

/** What is kept: what narrows the list and how it is ordered, not the page. */
const KEPT = [
  'q',
  'brand',
  'type',
  'class',
  'favourites',
  'custom',
  'assigned',
  'order',
  'desc',
] as const satisfies readonly (keyof ListQuery)[];

/** The kept part of a query, as the module keeps it and as it comes back. */
export const kept = (query: Readonly<Record<string, unknown>>): ListQuery =>
  Object.fromEntries(
    KEPT.filter((key) => query[key] !== undefined).map((key) => [
      key,
      query[key],
    ]),
  );
