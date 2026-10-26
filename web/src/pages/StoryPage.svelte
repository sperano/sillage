<!-- /task/...: one task's story, its timeline graph beside its event list and detail. -->
<script lang="ts">
	import type { Api } from '../lib/api';
	import { count } from '../lib/format';
	import type { LinkConfig } from '../lib/links';
	import { load } from '../lib/load.svelte';
	import type { Root } from '../lib/model';
	import { rootLabel } from '../lib/roots';
	import StoryView from '../lib/story/StoryView.svelte';
	import Timeline from '../lib/timeline/Timeline.svelte';
	import { zoneName, type Zone } from '../lib/timeline/time';
	import ErrorNotice from '../components/ErrorNotice.svelte';

	let { api, root, links = {} }: { api: Api; root: Root; links?: LinkConfig } = $props();

	const story = load(() => {
		const r = root;
		return (signal) => api.story(r, signal);
	});
	// UTC, as the overview, until the reader chooses local time in the graph's toolbar.
	let zone = $state<Zone>('utc');
</script>

<svelte:head><title>History of {rootLabel(root)} · Sillage</title></svelte:head>

<h1 tabindex="-1">History of {rootLabel(root)}</h1>
{#if story.current.state === 'loaded'}
	{@const s = story.current.value}
	<p>
		{count(s.events.length, 'event')}, {count(s.runs.length, 'run')},
		{count(s.pullRequests.length, 'pull request')} and
		{count(s.correlations.length, 'correlation')}.
	</p>
	<StoryView story={s} {links} timeZone={zoneName(zone)}>
		{#snippet graph(selection)}
			<Timeline story={s} {selection} bind:zone />
		{/snippet}
	</StoryView>
{:else if story.current.state === 'failed'}
	<ErrorNotice
		error={story.current.error}
		notFound="Sillage has no history of {rootLabel(root)}: it may not have been read yet."
	/>
	<p><a href="/">Search for another task</a></p>
{:else}
	<p role="status">Loading the history…</p>
{/if}
