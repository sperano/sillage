<script lang="ts">
	import type { Story } from '../model';
	import type { ItemRef } from '../timeline';
	import StoryView from '../story/StoryView.svelte';
	import FakeGraph from './FakeGraph.svelte';

	let { story, refs = [] }: { story: Story; refs?: ItemRef[] } = $props();
</script>

<StoryView {story}>
	{#snippet graph(selection)}
		<FakeGraph {selection} {refs} />
	{/snippet}
</StoryView>
