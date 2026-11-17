<script lang="ts">
	import type { Delivery } from '../decodeApi';
	import { deliveryLabel, outcomeText, RECEIPT_CAVEAT } from '../deliveries';
	import { SOURCE_NAMES } from '../format';
	import Time from './Time.svelte';

	// One webhook delivery attempt sillage received: what it named, what
	// sillage did with it, and the next read of the task from its source.

	let { delivery, timeZone }: { delivery: Delivery; timeZone: string } = $props();

	const source = $derived(SOURCE_NAMES[delivery.source]);
</script>

<h3 tabindex="-1">Webhook delivery: {deliveryLabel(delivery)}</h3>
<p>{RECEIPT_CAVEAT}</p>
<dl>
	<dt>Received by sillage</dt>
	<dd><Time at={delivery.receivedAt} {timeZone} /></dd>
	<dt>Event</dt>
	<dd>
		{#if delivery.event}
			<code>{delivery.event}</code>
		{:else}
			not known: sillage could not read the payload
		{/if}
	</dd>
	<dt>Outcome</dt>
	<dd data-testid="delivery-outcome">{outcomeText(delivery)}</dd>
	<dt>Next read</dt>
	<dd data-testid="next-read">
		{#if delivery.nextRead}
			<Time at={delivery.nextRead} {timeZone} />: the first read of the task from {source} that started
			after sillage received it, whether this delivery asked for it or it was due anyway
		{:else}
			none yet: no read of the task from {source} has started since sillage received it
		{/if}
	</dd>
	<dt>Delivery id</dt>
	<dd><code>{delivery.id}</code> on {delivery.instance} (a redelivery reuses it)</dd>
</dl>
<h4>Correlations</h4>
<p>None: no record of the story names this delivery, so it is linked to nothing.</p>
