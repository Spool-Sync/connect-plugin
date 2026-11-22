<!--
  A material drawn as a filament spool or a resin bottle, painted in the
  material's colour. Each comes as a drawing with dark outlines for the light
  theme and one with light outlines for the dark theme, and the theme shows
  one of the two. The dark theme's outlines are a light grey rather than
  white, which at list size outshone the colour.

  The drawings are the module's own files, so they reach the DOM whole, and
  the colour reaches them through a custom property, being a value from data.
  The accessible name says filament or resin; the row spells out the rest.
-->
<script lang="ts">
  import resinNeg from './resin-neg.svg?raw';
  import resinPos from './resin-pos.svg?raw';
  import spoolNeg from './spool-neg.svg?raw';
  import spoolPos from './spool-pos.svg?raw';

  import { cssColour } from '../css-colour';
  import { heldHost } from '../host';

  let {
    materialClass,
    colorRgba,
    class: className,
  }: {
    /** FFF draws a spool, SLA a bottle. */
    materialClass: string;
    /** The material's colour; none draws it in the muted text colour. */
    colorRgba: string | undefined;
    class?: string | undefined;
  } = $props();

  const t = heldHost().t;

  const resin = $derived(materialClass === 'SLA');
  const colour = $derived(cssColour(colorRgba) ?? 'var(--muted-foreground)');
</script>

<span
  role="img"
  aria-label={t(resin ? 'materials.filter.sla' : 'materials.filter.fff')}
  class={['inline-block shrink-0', className]}
  style="--material-colour: {colour};"
>
  <!-- eslint-disable svelte/no-at-html-tags -- the module's own drawings, never data. -->
  <span class="block size-full dark:hidden">
    {@html resin ? resinPos : spoolPos}
  </span>
  <span class="hidden size-full dark:block">
    {@html resin ? resinNeg : spoolNeg}
  </span>
  <!-- eslint-enable svelte/no-at-html-tags -->
</span>
