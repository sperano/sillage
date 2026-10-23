<!-- The entry page's freshness block: how far each source has been read. -->
<script lang="ts">
	import type { Api } from '../lib/api';
	import { load } from '../lib/load.svelte';
	import ErrorNotice from './ErrorNotice.svelte';
	import Freshness from './Freshness.svelte';

	let { api }: { api: Api } = $props();

	const uid = $props.id();
	const coverage = load(() => (signal) => api.coverage(signal));
</script>

<section aria-labelledby="{uid}-heading">
	<h2 id="{uid}-heading">Source freshness</h2>
	{#if coverage.current.state === 'loaded'}
		<Freshness coverage={coverage.current.value} />
	{:else if coverage.current.state === 'failed'}
		<ErrorNotice
			error={coverage.current.error}
			notFound="Sillage's server does not report its sources' freshness."
		/>
		<p>How recent the sources' records are is unknown.</p>
	{:else}
		<p role="status">Reading the sources' freshness…</p>
	{/if}
</section>
