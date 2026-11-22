<!--
  One value out of a long list, picked from a dropdown the person narrows by
  typing: the manufacturer and the type filters. The host's facet does the
  searching; the All row lets the filter go.
-->
<script lang="ts">
  import { FacetPopover, SelectFacet } from '@prusa/connect-ui/primitives';

  import { heldHost } from './host';

  interface Props {
    label: string;
    /** What the All row says. */
    allLabel: string;
    options: readonly { value: string; label: string }[];
    /** The picked value, and undefined for all. */
    value: string | undefined;
    onPick: (value: string | undefined) => void;
  }

  const { label, allLabel, options, value, onPick }: Props = $props();
  const t = heldHost().t;

  const picked = $derived(options.find((option) => option.value === value));
</script>

<FacetPopover {label} summary={picked?.label} width={272}>
  <SelectFacet
    mode="single"
    searchable
    searchPlaceholder={t('materials.filter.find')}
    {allLabel}
    options={[...options]}
    selected={value === undefined ? [] : [value]}
    onToggle={(chosen: string) => {
      onPick(chosen === '' ? undefined : chosen);
    }}
  />
</FacetPopover>
