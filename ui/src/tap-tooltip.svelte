<!--
  A tooltip that a pointer over its content opens, and so does a tap, since a
  touch screen has no hover. It hangs on a wrapper, so it also works on a
  disabled button, which takes no pointer. A disabled button takes no focus
  either, so a screen reader reads the text from hidden text in the wrapper.
  Several texts are drawn one to a line.
-->
<script lang="ts">
  import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
  } from '@prusa/connect-ui/primitives';

  import type { ClassValue } from 'svelte/elements';
  import type { Snippet } from 'svelte';

  let {
    text,
    class: className,
    children,
  }: {
    text: string | readonly string[];
    class?: ClassValue;
    children: Snippet;
  } = $props();

  const lines = $derived(typeof text === 'string' ? [text] : text);

  let open = $state(false);
</script>

<TooltipProvider>
  <Tooltip bind:open disableCloseOnTriggerClick>
    <TooltipTrigger>
      {#snippet child({ props })}
        <span
          {...props}
          data-part="tap-tooltip"
          class={['inline-flex items-center', className]}
          onclick={() => {
            open = !open;
          }}
        >
          {@render children()}
          <span class="sr-only">
            {#each lines as line, at (at)}<span class="block">{line}</span
              >{/each}
          </span>
        </span>
      {/snippet}
    </TooltipTrigger>
    <TooltipContent>
      {#each lines as line, at (at)}<span class="block">{line}</span>{/each}
    </TooltipContent>
  </Tooltip>
</TooltipProvider>
