<script lang="ts">
	import type { Interval } from '../model';
	import type { TimelineItem } from '../timeline';
	import { parseTime } from '../time';
	import Time from './Time.svelte';

	// When an item happened, as the event list shows it: an event's source
	// time, and its observed time when that differs; an interval's first
	// known bound; a phase's start, else its end, as its pod recorded it.

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
		seen <Time at={e.observedAt} {timeZone} /> (no source time)
	{/if}
{:else if item.kind === 'run'}
	{@render bounds(item.run.interval)}
{:else if item.kind === 'check'}
	{@render bounds(item.check.interval)}
{:else}
	{@const { start, end } = item.phase}
	{#if start}
		started <Time at={start.at} {timeZone} />
	{:else if end}
		start not recorded · ended <Time at={end.at} {timeZone} />
	{/if}
	(the pod's clock)
{/if}
