<script lang="ts">
	import type { Bound, Interval } from '../model';
	import { formatDuration, intervalDuration } from '../time';
	import Time from './Time.svelte';

	// An interval's ends as the sources recorded them, and its duration only
	// when both are known: dt and dd pairs, for use inside a dl.

	let { interval, timeZone }: { interval: Interval; timeZone: string } = $props();

	const duration = $derived(intervalDuration(interval));

	function why(b: Bound, end: string): string {
		return b.state === 'open' ? `the ${end} is open` : `the ${end} is unknown`;
	}

	const missing = $derived(
		[interval.start, interval.end]
			.map((b, i) => (b.state === 'known' ? undefined : why(b, i === 0 ? 'start' : 'end')))
			.filter((s) => s !== undefined)
			.join(' and ')
	);
</script>

{#snippet bound(b: Bound)}
	{#if b.state === 'known'}
		<Time at={b.at} {timeZone} />
	{:else if b.state === 'open'}
		open: not ended at the last successful read
	{:else}
		unknown: no source recorded it
	{/if}
{/snippet}

<dt>Start</dt>
<dd data-testid="start">{@render bound(interval.start)}</dd>
<dt>End</dt>
<dd data-testid="end">{@render bound(interval.end)}</dd>
<dt>Duration</dt>
<dd data-testid="duration">
	{#if duration !== undefined}
		{formatDuration(duration)}
	{:else}
		none: {missing || 'a recorded time could not be read'}
	{/if}
</dd>
