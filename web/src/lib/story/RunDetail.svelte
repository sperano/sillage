<script lang="ts">
	import type { Run, Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import type { TimelineItem } from '../timeline';
	import { phaseRef, runRef } from '../timeline';
	import { phaseText } from '../phases';
	import { runLink, type LinkConfig } from '../links';
	import { outcomeLabel, roleLabel, runLabel } from '../describe';
	import Bounds from './Bounds.svelte';
	import Correlations from './Correlations.svelte';
	import EvidenceList from './EvidenceList.svelte';
	import LinkButton from './LinkButton.svelte';
	import Time from './Time.svelte';

	let {
		story,
		items,
		selection,
		run,
		links,
		timeZone
	}: {
		story: Story;
		items: TimelineItem[];
		selection: Selection;
		run: Run;
		links: LinkConfig;
		timeZone: string;
	} = $props();

	const consoleLink = $derived(runLink(run, links));
	const USAGE_SEPARATOR = ' · ';

	/** The reported usage's values, with a separator only between them. */
	const usage = $derived.by(() => {
		const u = run.usage;
		const parts: string[] = [];
		if (u?.costUSD) parts.push(`US$${u.costUSD}`);
		if (u?.inputTokens !== undefined) parts.push(`${u.inputTokens} input tokens`);
		if (u?.outputTokens !== undefined) parts.push(`${u.outputTokens} output tokens`);
		return parts.join(USAGE_SEPARATOR);
	});
	const fields = $derived(
		(
			[
				['Task name', run.taskName],
				['Pod', run.pod],
				['Job', run.job],
				['Agent', run.agent],
				['Model', run.model],
				['Project', run.project],
				['Status message', run.message]
			] satisfies [string, string | undefined][]
		).filter((f): f is [string, string] => f[1] !== undefined)
	);
</script>

<h3 tabindex="-1">{runLabel(run)}</h3>
<dl>
	<dt>Run</dt>
	<dd>
		cluster {run.key.cluster}, namespace {run.key.namespace},
		{#if run.key.uid}
			Task UID <code>{run.key.uid}</code>
		{:else}
			pod <code>{run.key.pod}</code> (no Kelos object of it was read)
		{/if}
	</dd>
	<dt>Role</dt>
	<dd>{roleLabel(run.role)}</dd>
	<dt>Outcome</dt>
	<dd>{outcomeLabel(run.outcome)}</dd>
	{#each fields as [name, value] (name)}
		<dt>{name}</dt>
		<dd>{value}</dd>
	{/each}
	<dt>Task created</dt>
	<dd>
		{#if run.created}<Time at={run.created} {timeZone} />{:else}unknown{/if}
	</dd>
	<Bounds interval={run.interval} {timeZone} />
	{#if usage}
		<dt>Reported usage</dt>
		<dd data-testid="usage">{usage}</dd>
	{/if}
	<dt>Kelos console</dt>
	<dd>
		{#if consoleLink}
			<a href={consoleLink.href} rel="noreferrer">{consoleLink.label}</a>
			(the console finds the Task by name, which a retry reuses; a deleted Task is gone)
		{:else}
			none: no console is configured for cluster {run.key.cluster}, or the Task name is unknown
		{/if}
	</dd>
</dl>
<p>
	Times are the Kubernetes API server's and the Kelos controller's lifecycle times, not a measure of
	the agent's work.
</p>
{#if run.phases}
	<h4>Phases its pods reported</h4>
	<ul class="phases" data-testid="phases">
		{#each run.phases as phase (`${phase.podUID}/${phase.span}`)}
			<li>
				<LinkButton onclick={() => selection.select(phaseRef(run, phase))}
					>{phaseText(phase)}</LinkButton
				>
			</li>
		{/each}
	</ul>
	<p>Best effort: a missing marker is not a missing phase.</p>
{/if}
<h4>Evidence</h4>
<EvidenceList evidence={run.evidence} />
<Correlations {story} {items} {selection} subject={runRef(run)} />
