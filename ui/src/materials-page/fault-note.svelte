<!--
  What the module refused, by its code.

  The catalogue line for the code is the heading, the server's English detail
  is under it, and the code itself is beside them. A person quotes the code in
  a bug report and a reader in another language still gets a sentence
  (frontend-i18n.md rule 5).

  It draws nothing when nothing was refused, so a page can leave it in the
  markup and never wrap it in a branch of its own.
-->
<script lang="ts">
  import { FaultAlert } from '@prusa/connect-ui/primitives';

  import { faultText } from '../faults';
  import { heldHost } from '../host';

  let { fault }: { fault: unknown } = $props();

  const t = heldHost().t;
  const shown = $derived(fault === undefined ? undefined : faultText(fault, t));
</script>

<FaultAlert fault={shown} />
