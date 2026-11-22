import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import MaterialCircle from './material-circle.svelte';
import SpoolBar from './spool-bar.svelte';

// A colour comes from a printer, and a printer may send any text. A colour
// that is not #rrggbb or #rrggbbaa stays out of the style.
const hostile =
  'red; position: fixed; background-image: url(https://attacker.example/x)';

const added = (element: HTMLElement) => ({
  position: element.style.position,
  backgroundImage: element.style.backgroundImage,
});

describe('a colour that is not a colour', () => {
  it('adds no declaration to the material circle', async () => {
    await render(MaterialCircle, {
      text: 'PLA',
      colorRgba: hostile,
      size: 'small',
      title: 'Tool 1',
    });

    const circle = page.getByTestId('material-circle').element() as HTMLElement;

    expect(added(circle)).toEqual({ position: '', backgroundImage: '' });
  });

  it('adds no declaration to the spool bar', async () => {
    await render(SpoolBar, { left: 0.5, colorRgba: hostile });

    const bar = page.getByTestId('spool-bar').element() as HTMLElement;

    expect(added(bar)).toEqual({ position: '', backgroundImage: '' });
  });
});
