<!-- Each run's estimated cost: by attempt, model and thread, linked to its Task in the Kelos console. -->
<script lang="ts">
	import {
		estimateText,
		reportedText,
		runnerLabel,
		statusLabel,
		threadLabel
	} from '../lib/cost/describe';
	import type { RunCost } from '../lib/cost/model';
	import { outcomeLabel, roleLabel } from '../lib/describe';
	import { runKeyString } from '../lib/keys';
	import { runLink, type LinkConfig } from '../lib/links';
	import CostNotes from './CostNotes.svelte';

	let { runs, links }: { runs: readonly RunCost[]; links: LinkConfig } = $props();
</script>

<h3>By run</h3>
{#if runs.length === 0}
	<p>The task has no runs.</p>
{/if}
<ul class="cost-runs">
	{#each runs as run (runKeyString(run.key))}
		{@const consoleLink = runLink(run, links)}
		<li data-run={runKeyString(run.key)}>
			<h4>
				{roleLabel(run.role)}: {run.taskName ?? 'Task name unknown'}
			</h4>
			<dl>
				<dt>Outcome</dt>
				<dd>{outcomeLabel(run.outcome)}</dd>
				<dt>Estimate</dt>
				<dd data-testid="run-estimate">{estimateText(run)} ({statusLabel(run.status)})</dd>
				<dt>Reported by the runner</dt>
				<dd data-testid="run-reported">{reportedText(run)}</dd>
				<dt>Final</dt>
				<dd>{run.final ? 'yes' : 'not yet: more usage may still arrive'}</dd>
				{#if run.model}
					<dt>Configured model</dt>
					<dd>{run.model}</dd>
				{/if}
				<dt>Kelos console</dt>
				<dd>
					{#if consoleLink}
						<a href={consoleLink.href} rel="noreferrer">{consoleLink.label}</a>
					{:else}
						none: no console is configured for cluster {run.key.cluster}, or the Task name is
						unknown
					{/if}
				</dd>
			</dl>
			<CostNotes notes={run.notes} />
			{#if run.attempts.length > 0}
				<h5>Attempts</h5>
				<ul data-testid="run-attempts">
					{#each run.attempts as a (a.pod)}
						<li>pod <code>{a.pod}</code>: {estimateText(a)}</li>
					{/each}
				</ul>
			{/if}
			{#if run.models.length > 0}
				<h5>Models</h5>
				<ul data-testid="run-models">
					{#each run.models as m, i (i)}
						<li>
							{m.model || 'model not reported'} ({runnerLabel(m.runner)}{m.tier
								? `, ${m.tier}`
								: ''}):
							{estimateText(m)}
						</li>
					{/each}
				</ul>
			{/if}
			{#if run.threads.length > 0}
				<h5>Threads</h5>
				<ul data-testid="run-threads">
					{#each run.threads as t, i (i)}
						<li>{threadLabel(t)}: {estimateText(t)}</li>
					{/each}
				</ul>
			{/if}
		</li>
	{/each}
</ul>

<style>
	.cost-runs {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.cost-runs > li {
		border-top: 1px solid var(--color-border);
		padding: var(--space-2) 0;
	}
	h4,
	h5 {
		margin: var(--space-2) 0 var(--space-1);
	}
	dl {
		display: grid;
		grid-template-columns: max-content minmax(0, 1fr);
		gap: var(--space-1) var(--space-4);
		margin: 0;
	}
	dt {
		font-weight: 600;
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
</style>
