<script lang="ts">
	import { tick } from 'svelte';
	import type { Deliveries, Delivery } from '../decodeApi';
	import {
		deliveriesAround,
		deliveryKey,
		deliveryLabel,
		deliveryRef,
		NEARBY_MINUTES,
		RECEIPT_CAVEAT
	} from '../deliveries';
	import { errorText } from '../errorText';
	import type { Load } from '../load.svelte';
	import type { Selection } from '../selection.svelte';
	import LinkButton from './LinkButton.svelte';
	import Time from './Time.svelte';

	// The webhook deliveries sillage received around a state change, listed
	// by time alone: no record names a delivery, so none is its cause.

	let {
		deliveries,
		at,
		selection,
		timeZone
	}: { deliveries: Load<Deliveries>; at: number; selection: Selection; timeZone: string } =
		$props();

	const nearby = $derived(
		deliveries.state === 'loaded' ? deliveriesAround(deliveries.value.deliveries, at) : []
	);

	/** Selects the delivery and moves the focus to its detail's heading, which replaces this one. */
	async function choose(event: MouseEvent, d: Delivery) {
		const panel = (event.currentTarget as HTMLElement).closest('section');
		selection.select(deliveryRef(d));
		await tick();
		panel?.querySelector<HTMLElement>('h3')?.focus();
	}
</script>

{#if deliveries.state !== 'idle'}
	<div data-nearby-deliveries>
		<h4>Webhook deliveries around it</h4>
		<p>
			{RECEIPT_CAVEAT} Listed by time alone, received within {NEARBY_MINUTES} minutes either way: no record
			of the story names a delivery, so none is linked to this change.
		</p>
		{#if deliveries.state === 'loading'}
			<p role="status">Loading the deliveries…</p>
		{:else if deliveries.state === 'failed'}
			<p>
				{errorText(deliveries.error, 'Sillage has no deliveries of this task.')} What sillage received
				is unknown here.
			</p>
		{:else if nearby.length === 0}
			<p data-testid="no-delivery">
				Sillage recorded none. That is unknown, not a failure: a delivery may have gone to Kelos
				alone, or reached sillage when it recorded none.
			</p>
		{:else}
			<ul>
				{#each nearby as d (deliveryKey(d))}
					<li>
						<Time at={d.receivedAt} {timeZone} />:
						<LinkButton onclick={(e) => choose(e, d)}>{deliveryLabel(d)}</LinkButton>, {d.outcome};
						{#if d.nextRead}
							next read <Time at={d.nextRead} {timeZone} />
						{:else}
							not read since
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
		{#if deliveries.state === 'loaded' && deliveries.value.truncated}
			<p>Only the latest deliveries were loaded: older ones are not listed.</p>
		{/if}
	</div>
{/if}
