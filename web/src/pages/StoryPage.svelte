<!-- /task/... and /page/...: one root's story. The timeline and the event list come later. -->
<script lang="ts">
	import type { Api } from '../lib/api';
	import { count } from '../lib/format';
	import { load } from '../lib/load.svelte';
	import type { Root } from '../lib/model';
	import { rootLabel } from '../lib/roots';
	import ErrorNotice from '../components/ErrorNotice.svelte';
	import Freshness from '../components/Freshness.svelte';

	let { api, root }: { api: Api; root: Root } = $props();

	const story = load(() => {
		const r = root;
		return (signal) => api.story(r, signal);
	});
</script>

<h1>History of {rootLabel(root)}</h1>
{#if story.current.state === 'loaded'}
	{@const s = story.current.value}
	<p>
		{count(s.events.length, 'event')}, {count(s.runs.length, 'run')},
		{count(s.pullRequests.length, 'pull request')} and
		{count(s.correlations.length, 'correlation')}.
	</p>
	<p>The timeline and the event list are not built yet.</p>
	<section aria-labelledby="story-sources-heading">
		<h2 id="story-sources-heading">Sources</h2>
		<Freshness coverage={s.coverage} />
	</section>
{:else if story.current.state === 'failed'}
	<ErrorNotice
		error={story.current.error}
		notFound="Sillage has no history of {rootLabel(root)}: it may not have been read yet."
	/>
	<p><a href="/">Search for another task or page</a></p>
{:else}
	<p role="status">Loading the history…</p>
{/if}
