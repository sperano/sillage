<script lang="ts">
	import type { Deliveries } from '../decodeApi';
	import { findDelivery } from '../deliveries';
	import type { Load } from '../load.svelte';
	import type { Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import { findItem, type TimelineItem } from '../timeline';
	import type { LinkConfig } from '../links';
	import CheckDetail from './CheckDetail.svelte';
	import DeliveryDetail from './DeliveryDetail.svelte';
	import EventDetail from './EventDetail.svelte';
	import PhaseDetail from './PhaseDetail.svelte';
	import RunDetail from './RunDetail.svelte';

	let {
		story,
		items,
		selection,
		links,
		timeZone,
		deliveries = { state: 'idle' }
	}: {
		story: Story;
		items: TimelineItem[];
		selection: Selection;
		links: LinkConfig;
		timeZone: string;
		/** The root's webhook deliveries: a delivery selected in the graph is one of them. */
		deliveries?: Load<Deliveries>;
	} = $props();

	const item = $derived(selection.current && findItem(items, selection.current));
	const delivery = $derived(
		selection.current && deliveries.state === 'loaded'
			? findDelivery(deliveries.value.deliveries, selection.current)
			: undefined
	);
</script>

<section class="detail-panel surface" aria-labelledby="detail-heading">
	<h2 id="detail-heading">Detail</h2>
	{#if delivery}
		<DeliveryDetail {delivery} {timeZone} />
	{:else if !item}
		<p>Select an event, a run, a check or a phase to see its details.</p>
	{:else if item.kind === 'event'}
		<EventDetail {story} {items} {selection} event={item.event} {links} {timeZone} {deliveries} />
	{:else if item.kind === 'run'}
		<RunDetail {story} {items} {selection} run={item.run} {links} {timeZone} />
	{:else if item.kind === 'check'}
		<CheckDetail pr={item.pr} check={item.check} {timeZone} />
	{:else}
		<PhaseDetail run={item.run} phase={item.phase} {selection} {timeZone} />
	{/if}
</section>

<style>
	.detail-panel :global(dl) {
		display: grid;
		grid-template-columns: max-content minmax(0, 1fr);
		gap: 0.25rem 1rem;
		margin: 0;
	}

	.detail-panel :global(dd) {
		margin: 0;
		overflow-wrap: anywhere;
	}

	.detail-panel :global(dt) {
		font-weight: 600;
	}
</style>
