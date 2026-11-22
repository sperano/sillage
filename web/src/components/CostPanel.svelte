<!--
	A task's estimated API-equivalent cost, under the stable anchor #cost:
	its total, by category, role and model, and each run's. Loading and a
	failure are this section's own: the rest of the page shows without it.
-->
<script lang="ts">
	import { COST_ANCHOR, type TaskCost } from '../lib/cost/model';
	import { errorText } from '../lib/errorText';
	import type { LinkConfig } from '../lib/links';
	import type { Load } from '../lib/load.svelte';
	import CostRuns from './CostRuns.svelte';
	import CostSummary from './CostSummary.svelte';
	import CostTables from './CostTables.svelte';

	let { cost, links, timeZone }: { cost: Load<TaskCost>; links: LinkConfig; timeZone: string } =
		$props();
</script>

<section id={COST_ANCHOR} class="surface cost" aria-labelledby="cost-heading">
	<h2 id="cost-heading">Estimated cost</h2>
	{#if cost.state === 'loaded'}
		<CostSummary cost={cost.value} {timeZone} />
		<CostTables cost={cost.value} />
		<CostRuns runs={cost.value.runs} {links} />
	{:else if cost.state === 'failed'}
		<p data-testid="cost-failed">
			{errorText(cost.error, 'Sillage has no cost of this task.')} The estimated cost is not shown.
		</p>
	{:else}
		<p role="status">Loading the estimated cost…</p>
	{/if}
</section>

<style>
	.cost {
		margin-top: var(--space-4);
		overflow-x: auto;
	}
</style>
