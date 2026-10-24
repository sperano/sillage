<script lang="ts">
	import type { Phase, PhaseMark, PhaseState, Run } from '../model';
	import { endText, kindName, phaseText, stateText } from '../phases';
	import type { Selection } from '../selection.svelte';
	import { runName } from '../describe';
	import { formatDuration, intervalDuration } from '../time';
	import { runRef } from '../timeline';
	import EvidenceList from './EvidenceList.svelte';
	import Time from './Time.svelte';

	// One phase of a run: what its pod's markers recorded, what that
	// establishes, and how the markers were joined to the run.

	let {
		run,
		phase,
		selection,
		timeZone
	}: { run: Run; phase: Phase; selection: Selection; timeZone: string } = $props();

	const why: Record<PhaseState, string> = {
		complete: 'both edges recorded',
		'in-progress': 'only its start recorded, and the run had not ended at the last read',
		interrupted:
			'only its start recorded, and the run is no longer running: it is never closed at the run’s end',
		'start-not-recorded': 'only its end recorded: when it started is unknown',
		inconsistent:
			'its end is before its start, or of another kind: no interval is drawn, both edges are kept'
	};

	const kinds = $derived.by(() => {
		const { start, end } = phase;
		if (start && end && start.kind !== end.kind) {
			return `${kindName(start.kind)} start, ${kindName(end.kind)} end`;
		}
		return kindName((start ?? end)!.kind);
	});
	const emitters = $derived(
		[...new Set([phase.start?.emitter, phase.end?.emitter].filter((e) => e !== undefined))].join(
			', '
		)
	);
	const duration = $derived(
		phase.state === 'complete' && phase.interval ? intervalDuration(phase.interval) : undefined
	);
	const openEnd = $derived(phase.interval?.end.state === 'open');
</script>

{#snippet edge(mark: PhaseMark | undefined, missing: string)}
	{#if mark}
		<Time at={mark.at} {timeZone} />
	{:else}
		{missing}
	{/if}
{/snippet}

<h3 tabindex="-1">{phaseText(phase)}</h3>
<dl>
	<dt>Run</dt>
	<dd>
		<button type="button" class="link" onclick={() => selection.select(runRef(run))}
			>{runName(run)}</button
		>
	</dd>
	<dt>Phase</dt>
	<dd data-testid="phase-state">{stateText(phase.state)}: {why[phase.state]}</dd>
	<dt>Kind</dt>
	<dd>{kinds}</dd>
	<dt>Outcome</dt>
	<dd>{phase.end ? endText(phase.end) : 'none recorded'}</dd>
	<dt>Start</dt>
	<dd data-testid="phase-start">{@render edge(phase.start, 'not recorded')}</dd>
	<dt>End</dt>
	<dd data-testid="phase-end">
		{@render edge(phase.end, openEnd ? 'open: not ended at the last read' : 'not recorded')}
	</dd>
	<dt>Duration</dt>
	<dd>
		{#if duration !== undefined}
			{formatDuration(duration)}
		{:else}
			none: only a phase with both edges recorded, paired, has one
		{/if}
	</dd>
	<dt>Reported by</dt>
	<dd>{emitters}, in pod <code>{phase.pod}</code> (uid <code>{phase.podUID}</code>)</dd>
	<dt>Span</dt>
	<dd><code>{phase.span}</code></dd>
	<dt>Joined to the run</dt>
	<dd data-testid="phase-join">
		{#if phase.confidence === 'identifier'}
			by its pod: a Kelos Task’s status named the pod the marker’s token was verified for
		{:else}
			uncertain: by the Task name the marker gives and its time, inside one run’s interval; no Kelos
			read named its pod
		{/if}
	</dd>
</dl>
<p>
	Times are the pod’s clock, which differs from the API server’s by NTP error only: a phase can sit
	just outside the run’s lifecycle times. Markers are best effort: a missing marker is not a missing
	phase, and the time between two phases is not idle.
</p>
<h4>Evidence</h4>
<EvidenceList evidence={phase.evidence} />

<style>
	.link {
		padding: 0;
		border: none;
		background: none;
		color: LinkText;
		font: inherit;
		text-decoration: underline;
		cursor: pointer;
	}
</style>
