<!-- The entry page's freshness block: how far each source has been read. -->
<script lang="ts">
	import type { Api } from '../lib/api';
	import { load } from '../lib/load.svelte';
	import ErrorNotice from './ErrorNotice.svelte';
	import FreshnessSummary from './FreshnessSummary.svelte';

	let { api }: { api: Api } = $props();

	const uid = $props.id();
	const freshness = load(() => (signal) => api.freshness(signal));
</script>

<section aria-labelledby="{uid}-heading">
	<h2 id="{uid}-heading">Source freshness</h2>
	{#if freshness.current.state === 'loaded'}
		<FreshnessSummary sources={freshness.current.value} />
	{:else if freshness.current.state === 'failed'}
		<ErrorNotice
			error={freshness.current.error}
			notFound="Sillage's server does not report its sources' freshness."
		/>
		<p>How recent the sources' records are is unknown.</p>
	{:else}
		<p role="status">Reading the sources' freshness…</p>
	{/if}
</section>
