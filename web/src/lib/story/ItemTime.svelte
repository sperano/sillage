<script lang="ts">
	import type { TimelineItem } from '../timeline';
	import { knownTime, parseTime } from '../time';
	import Time from './Time.svelte';

	// When an item happened, as the event list shows it: an event's source
	// time, and its observed time when that differs; an interval's first
	// known bound.

	let { item, timeZone }: { item: TimelineItem; timeZone: string } = $props();
</script>

{#snippet bounds(start: string | undefined, end: string | undefined)}
	{#if start}
		started <Time at={start} {timeZone} />
	{:else if end}
		start unknown · ended <Time at={end} {timeZone} />
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
	{@render bounds(knownTime(item.run.interval.start), knownTime(item.run.interval.end))}
{:else}
	{@render bounds(knownTime(item.check.interval.start), knownTime(item.check.interval.end))}
{/if}
