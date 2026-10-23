<script lang="ts">
	import type { Interval } from '../model';
	import type { TimelineItem } from '../timeline';
	import { knownTime } from '../time';
	import Time from './Time.svelte';

	// When an item happened, as the event list shows it: an event's source
	// time, and its observed time when that differs; an interval's first
	// known bound.

	let { item, timeZone }: { item: TimelineItem; timeZone: string } = $props();

	function interval(i: TimelineItem): Interval | undefined {
		return i.kind === 'run' ? i.run.interval : i.kind === 'check' ? i.check.interval : undefined;
	}

	const span = $derived(interval(item));
</script>

{#if item.kind === 'event'}
	{@const e = item.event}
	{#if e.sourceTime}
		<Time at={e.sourceTime} {timeZone} />
		{#if Date.parse(e.sourceTime) !== Date.parse(e.observedAt)}
			· observed <Time at={e.observedAt} {timeZone} />
		{/if}
	{:else}
		seen <Time at={e.observedAt} {timeZone} /> (no source time)
	{/if}
{:else if span}
	{@const start = knownTime(span.start)}
	{@const end = knownTime(span.end)}
	{#if start}
		started <Time at={start} {timeZone} />
	{:else if end}
		start unknown · ended <Time at={end} {timeZone} />
	{:else}
		start and end unknown
	{/if}
{/if}
