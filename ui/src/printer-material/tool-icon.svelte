<!--
  One of the printer icons, in the text colour around it: the nozzle, the NFC
  tag, a spool from Materials and the info mark. The drawings are SVG files
  copied in, so they reach the DOM whole.
-->
<script lang="ts">
  import info from './icons/info.svg?raw';
  import nfc from './icons/nfc.svg?raw';
  import nozzle from './icons/nozzle.svg?raw';
  import spool from './icons/spool_color.svg?raw';

  const DRAWINGS = { info, nfc, nozzle, spool } as const;

  let {
    name,
    title,
    class: className,
  }: {
    name: keyof typeof DRAWINGS;
    /** Names the icon to a screen reader; without it the icon is decoration. */
    title?: string | undefined;
    class?: string | undefined;
  } = $props();
</script>

<span
  class={['icon inline-block shrink-0', className]}
  role={title === undefined ? undefined : 'img'}
  aria-label={title}
  aria-hidden={title === undefined ? 'true' : undefined}
  {title}
>
  <!-- eslint-disable-next-line svelte/no-at-html-tags -- the module's own drawings, never data. -->
  {@html DRAWINGS[name]}
</span>

<style>
  .icon :global(svg) {
    display: block;
    inline-size: 100%;
    block-size: 100%;
  }
</style>
