<!--
  The colour families a list narrows to, drawn as a grid of swatches. It is the
  local equivalent of the library's FilamentColorWheel, which the published
  @prusa/connect-ui 0.1.0-beta.1 does not export: the same props, one button a
  family, the picked one pressed. The outer ring keeps its order at the top.
-->
<script lang="ts" generics="T extends readonly string[]">
  let {
    colors,
    outerCount,
    selected,
    labelOf,
    onChoose,
  }: {
    colors: readonly T[];
    /** How many of the first families are the outer ring. */
    outerCount: number;
    selected: T | undefined;
    labelOf: (family: T) => string;
    onChoose: (family: T) => void;
  } = $props();
</script>

<div class="grid grid-cols-6 gap-1.5" role="group">
  {#each colors as family, index (family[0])}
    {@const label = labelOf(family)}
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={selected === family}
      data-ring={index < outerCount ? 'outer' : 'inner'}
      class="size-6 rounded-full border border-border outline-none focus-visible:ring-2 focus-visible:ring-ring aria-pressed:ring-2 aria-pressed:ring-ring"
      style:background-color={family[1]}
      onclick={() => onChoose(family)}
    ></button>
  {/each}
</div>
