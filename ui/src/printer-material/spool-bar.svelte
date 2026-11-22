<!--
  What is left of a spool as a bar in the material's colour: a thin rule on a
  tool's tile and a thicker bar on the one-tool view. The track is always
  drawn; the fill only when the share left is known.
-->
<script lang="ts">
  import { cssColour } from '../css-colour';

  let {
    left,
    colorRgba,
    thick = false,
  }: {
    /** The share of the filament left, 0 to 1, or undefined when unknown. */
    left: number | undefined;
    colorRgba: string | undefined;
    thick?: boolean;
  } = $props();
</script>

<span
  data-part="spool-bar"
  class={['track block w-full overflow-hidden', thick && 'thick']}
  style="--left: {left ?? 0}; --colour: {cssColour(colorRgba) ??
    'var(--color-foreground)'};"
>
  {#if left !== undefined}
    <span class="fill block h-full"></span>
  {/if}
</span>

<style>
  .track {
    block-size: 2px;
    background: var(--color-border);
  }

  .thick {
    block-size: calc(var(--spacing) * 2);
    border-radius: var(--radius-sm);
  }

  .fill {
    inline-size: calc(clamp(0, var(--left), 1) * 100%);
    background: var(--colour);
  }
</style>
