<!-- A breakdown's notes, each in a sentence; nothing when it has none. -->
<script lang="ts">
	import { noteText } from '../lib/cost/describe';
	import type { CostNote } from '../lib/cost/model';

	let { notes }: { notes: readonly CostNote[] } = $props();
</script>

{#if notes.length > 0}
	<ul class="cost-notes" data-testid="cost-notes">
		{#each notes as note (note)}
			<li data-note={note}>{noteText(note)}</li>
		{/each}
	</ul>
{/if}

<style>
	.cost-notes {
		margin: var(--space-1) 0;
		padding-left: var(--space-6);
		color: var(--color-muted);
		font-size: var(--font-size-sm);
	}
</style>
