<script lang="ts">
	import type { Selection } from '../selection.svelte';
	import type { ItemRef } from '../timeline';

	// A stand-in for the timeline graph: it shows what is selected and can
	// select, through the selection StoryView gives it.

	let { selection, refs }: { selection: Selection; refs: ItemRef[] } = $props();
</script>

<div data-testid="graph" style="height: 200px">
	<output data-testid="graph-selected">{selection.current?.id ?? ''}</output>
	{#each refs as r (`${r.kind}:${r.id}`)}
		<button type="button" onclick={() => selection.select(r)}>graph {r.id}</button>
	{/each}
</div>
