<!-- A task's estimated cost in total: what it is, how sure, and what it rests on. -->
<script lang="ts">
	import { estimateText, reportedText, statusLabel } from '../lib/cost/describe';
	import type { TaskCost } from '../lib/cost/model';
	import { count } from '../lib/format';
	import Time from '../lib/story/Time.svelte';
	import CostNotes from './CostNotes.svelte';

	let { cost, timeZone }: { cost: TaskCost; timeZone: string } = $props();

	const total = $derived(cost.total);
</script>

<p class="cost-total">
	<strong data-testid="cost-total">{estimateText(total)}</strong>
	<span data-testid="cost-status">({statusLabel(total.status)})</span>
</p>
<p>
	What the agents' usage would have cost at the providers' public API list prices: not what was
	billed, nor money saved.
</p>
<dl class="cost-facts">
	<dt>Reported by the runners</dt>
	<dd data-testid="cost-reported">
		{reportedText(total)}
		{#if total.reportedUSD}(their own estimate, shown apart: never added to the one above){/if}
	</dd>
	<dt>Usage</dt>
	<dd data-testid="cost-requests">
		{count(total.requests, 'request')}{#if total.failedRequests > 0}, {total.failedRequests} of them failed{/if}
	</dd>
	<dt>Final</dt>
	<dd data-testid="cost-final">
		{#if cost.final}
			yes: every run ended and its usage was read
		{:else}
			not yet: more usage may still arrive
		{/if}
	</dd>
	<dt>Usage feed</dt>
	<dd data-testid="cost-feed">
		{#if !cost.feed.on}
			off: sillage reads no usage
		{:else if cost.feed.readThrough}
			read through <Time at={cost.feed.readThrough} {timeZone} />
		{:else}
			on, nothing read yet
		{/if}
		{#if cost.feed.since}
			(the runners' telemetry started <Time at={cost.feed.since} {timeZone} />){/if}
	</dd>
	{#if cost.unattributed}
		<dt>Not joined to a run</dt>
		<dd data-testid="cost-unattributed">
			{estimateText(cost.unattributed)}, counted in the total
		</dd>
	{/if}
	<dt>Price table</dt>
	<dd data-testid="cost-prices">
		version {cost.prices.version}, verified {cost.prices.verifiedAt}
	</dd>
	<dt>Prices from</dt>
	<dd>
		<ul class="cost-sources">
			{#each cost.prices.sources as s (s.url)}
				<li><a href={s.url} rel="noreferrer">{s.provider}</a> (read {s.readAt})</li>
			{/each}
		</ul>
	</dd>
</dl>
<div data-testid="cost-total-notes"><CostNotes notes={total.notes} /></div>
<h3>Assumptions</h3>
<ul data-testid="cost-assumptions">
	{#each cost.assumptions as a (a.code)}
		<li data-assumption={a.code}>{a.text}</li>
	{/each}
</ul>
{#if cost.assumptions.length === 0}
	<p>The price table states none.</p>
{/if}

<style>
	.cost-total {
		font-size: var(--font-size-lg);
		margin: 0 0 var(--space-1);
	}
	.cost-facts {
		display: grid;
		grid-template-columns: max-content minmax(0, 1fr);
		gap: var(--space-1) var(--space-4);
		margin: var(--space-2) 0;
	}
	.cost-facts dt {
		font-weight: 600;
	}
	.cost-facts dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.cost-sources {
		margin: 0;
		padding: 0;
		list-style: none;
	}
</style>
