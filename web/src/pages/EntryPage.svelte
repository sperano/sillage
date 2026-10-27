<!-- /: search, paste a link, and how fresh the sources are. -->
<script lang="ts">
	import type { Api } from '../lib/api';
	import type { Router } from '../lib/router.svelte';
	import OpenLinkForm from '../components/OpenLinkForm.svelte';
	import SearchSection from '../components/SearchSection.svelte';
	import SourceFreshness from '../components/SourceFreshness.svelte';

	let { api, router, query }: { api: Api; router: Router; query: string } = $props();
</script>

<svelte:head><title>Sillage</title></svelte:head>

<h1 tabindex="-1">Sillage</h1>
<p>What people, agents and automation did on a task, from Vikunja, Kelos and GitHub.</p>
<p>
	<a href="/overview">What was working on what?</a> The tasks active over a window, side by side.
</p>
<SearchSection {api} {router} {query} />
<OpenLinkForm {router} />
<SourceFreshness {api} />
