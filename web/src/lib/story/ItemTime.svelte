<script lang="ts">
	import type { Interval } from '../model';
	import type { TimelineItem } from '../timeline';
	import { compareTimes } from '../check';
	import { parseTime } from '../time';
	import Time from './Time.svelte';

	// When an item happened, as the event list shows it: an event's source
	// time, and its observed time when that differs, or when a state seen
	// by polling was first and last seen; an interval's first known bound;
	// a phase's start, else its end, as its pod recorded it, and both edges
	// of inconsistent markers, earlier first, as the list orders them.

	let { item, timeZone }: { item: TimelineItem; timeZone: string } = $props();
</script>

{#snippet bounds({ start, end }: Interval)}
	{#if start.state === 'known'}
		started <Time at={start.at} {timeZone} />
	{:else if end.state === 'known'}
		start unknown · ended <Time at={end.at} {timeZone} />
	{:else if end.state === 'open'}
		start unknown · not ended at the last read
	{:else}
		start and end unknown
	{/if}
{/snippet}

{#if item.kind === 'event'}
	{@const e = item.event}
	{#if e.sourceTime}
		<Time at={e.sourceTime} {timeZone} />
		{#if parseTime(e.sourceTime) !== parseTime(e.observedAt)}
			· observed <Time at={e.observedAt} {timeZone} />
		{/if}
	{:else}
		seen <Time at={e.observedAt} {timeZone} />
		{#if e.lastObservedAt}
			· last seen <Time at={e.lastObservedAt} {timeZone} />
		{/if}
		(no source time)
	{/if}
{:else if item.kind === 'run'}
	{@render bounds(item.run.interval)}
{:else if item.kind === 'check'}
	{@render bounds(item.check.interval)}
{:else}
	{@const p = item.phase}
	{#if p.state === 'start-not-recorded'}
		start not recorded · ended <Time at={p.end.at} {timeZone} />
	{:else if p.state === 'inconsistent'}
		{#if compareTimes(p.end.at, p.start.at) < 0}
			end <Time at={p.end.at} {timeZone} /> · start <Time at={p.start.at} {timeZone} />
		{:else}
			start <Time at={p.start.at} {timeZone} /> · end <Time at={p.end.at} {timeZone} />
		{/if}
	{:else}
		started <Time at={p.start.at} {timeZone} />
	{/if}
	(the pod's clock)
{/if}
