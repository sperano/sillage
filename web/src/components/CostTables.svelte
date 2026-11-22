<!-- A task's estimated cost by token category, by role and by model. -->
<script lang="ts">
	import {
		categoryLabel,
		estimateText,
		reportedText,
		runnerLabel,
		statusLabel,
		usd
	} from '../lib/cost/describe';
	import type { CategoryCost, TaskCost } from '../lib/cost/model';
	import { roleLabel } from '../lib/describe';

	let { cost }: { cost: TaskCost } = $props();

	/** A category's priced part: unknown, or a lower bound, when some of its tokens are unpriced. */
	function categoryText(c: CategoryCost): string {
		if (c.estimatedUSD === undefined) return c.unpriced ? 'unknown' : 'none';
		return c.unpriced ? `at least ${usd(c.estimatedUSD)}` : usd(c.estimatedUSD);
	}
</script>

<table data-testid="cost-categories">
	<caption>Tokens by category</caption>
	<thead>
		<tr>
			<th scope="col">Category</th>
			<th scope="col">Tokens</th>
			<th scope="col">Estimate</th>
		</tr>
	</thead>
	<tbody>
		{#each cost.total.categories as c (c.category)}
			<tr>
				<th scope="row">{categoryLabel(c.category)}</th>
				<td class="number">{c.tokens}</td>
				<td>{categoryText(c)}</td>
			</tr>
		{/each}
	</tbody>
</table>

<table data-testid="cost-by-role">
	<caption>By role</caption>
	<thead>
		<tr>
			<th scope="col">Role</th>
			<th scope="col">Runs</th>
			<th scope="col">Estimate</th>
			<th scope="col">Status</th>
			<th scope="col">Reported</th>
		</tr>
	</thead>
	<tbody>
		{#each cost.byRole as r (r.role)}
			<tr data-role={r.role}>
				<th scope="row">{roleLabel(r.role)}</th>
				<td class="number">{r.runs}</td>
				<td>{estimateText(r)}</td>
				<td>{statusLabel(r.status)}</td>
				<td>{reportedText(r)}</td>
			</tr>
		{/each}
	</tbody>
</table>

<table data-testid="cost-by-model">
	<caption>By model</caption>
	<thead>
		<tr>
			<th scope="col">Model</th>
			<th scope="col">Runner</th>
			<th scope="col">Tier</th>
			<th scope="col">Requests</th>
			<th scope="col">Estimate</th>
			<th scope="col">Status</th>
			<th scope="col">Reported</th>
		</tr>
	</thead>
	<tbody>
		{#each cost.byModel as m, i (i)}
			<tr>
				<th scope="row">{m.model || 'not reported'}</th>
				<td>{runnerLabel(m.runner)}</td>
				<td>{m.tier ?? 'unknown'}</td>
				<td class="number">{m.requests}</td>
				<td>{estimateText(m)}</td>
				<td>{statusLabel(m.status)}</td>
				<td>{reportedText(m)}</td>
			</tr>
		{/each}
	</tbody>
</table>

<style>
	table {
		border-collapse: collapse;
		margin: var(--space-3) 0;
		font-size: var(--font-size-sm);
	}
	caption {
		text-align: left;
		font-weight: 600;
		padding-bottom: var(--space-1);
	}
	th,
	td {
		border-bottom: 1px solid var(--color-border);
		padding: var(--space-1) var(--space-3) var(--space-1) 0;
		text-align: left;
	}
	thead th {
		color: var(--color-muted);
	}
	.number {
		text-align: right;
		font-variant-numeric: tabular-nums;
	}
</style>
