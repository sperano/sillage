<!-- /: search, paste a link, and how fresh the sources are. -->
<script lang="ts">
	import type { Api } from '../lib/api';
	import type { Router } from '../lib/router.svelte';
	import OpenLinkForm from '../components/OpenLinkForm.svelte';
	import SearchSection from '../components/SearchSection.svelte';
	import SourceFreshness from '../components/SourceFreshness.svelte';

	let { api, router, query }: { api: Api; router: Router; query: string } = $props();
</script>

<h1>Sillage</h1>
<p>What people, agents and automation did on a task, from Vikunja, Kelos, GitHub and the wiki.</p>
<SearchSection {api} {router} {query} />
<OpenLinkForm {router} />
<SourceFreshness {api} />
