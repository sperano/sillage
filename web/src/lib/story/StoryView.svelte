<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { Story } from '../model';
	import type { LinkConfig } from '../links';
	import { Selection } from '../selection.svelte';
	import { DEFAULT_TIME_ZONE } from '../time';
	import { timelineItems } from '../timeline';
	import CoverageNotices from './CoverageNotices.svelte';
	import DetailPanel from './DetailPanel.svelte';
	import EventList from './EventList.svelte';
	import NextAction from './NextAction.svelte';

	// One story: coverage notices and the next action above the graph, the
	// event list and the detail panel beside it, stacked under it on narrow
	// screens. The graph is given the same selection as the panels.

	let {
		story,
		links = {},
		timeZone = DEFAULT_TIME_ZONE,
		selection = new Selection(),
		graph
	}: {
		story: Story;
		links?: LinkConfig;
		timeZone?: string;
		selection?: Selection;
		graph?: Snippet<[Selection]>;
	} = $props();

	const items = $derived(timelineItems(story));
</script>

<div class="story">
	<CoverageNotices {story} {timeZone} />
	<NextAction {story} {timeZone} />
	<div class="layout" class:with-graph={graph !== undefined}>
		{#if graph}
			<div class="graph">{@render graph(selection)}</div>
		{/if}
		<div class="panels">
			<EventList {story} {items} {selection} {links} {timeZone} />
			<DetailPanel {story} {items} {selection} {links} {timeZone} />
		</div>
	</div>
</div>

<style>
	.story,
	.layout,
	.panels {
		display: grid;
		gap: var(--space-4);
	}
	.panels {
		align-content: start;
	}

	/* Wide: the graph, then the panels beside it in one column. */
	.layout.with-graph {
		grid-template-columns: minmax(0, 2fr) minmax(18rem, 1fr);
	}

	/* No graph: the list and the detail side by side. */
	.layout:not(.with-graph) .panels {
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
	}

	/* Narrow (below 48rem): everything in one column, the panels under the graph. */
	@media (max-width: 48rem) {
		.layout.with-graph,
		.layout:not(.with-graph) .panels {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
