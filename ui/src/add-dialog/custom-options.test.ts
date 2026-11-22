import { describe, expect, it } from 'vitest';

import english from '../locales/en.json';
import { TAGS } from './custom-options';

describe('the tags a custom material offers', () => {
  it('each reads from the catalogue, so a translator reaches it', () => {
    const missing = TAGS.filter(
      (name) => !(`materials.tag.${name}` in english),
    );
    expect(missing).toEqual([]);
  });
});
