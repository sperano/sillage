<!-- Any other path. -->
<script lang="ts">
	let { path }: { path: string } = $props();
</script>

<svelte:head><title>Not found · Sillage</title></svelte:head>

<h1 tabindex="-1">Not found</h1>
<p>Sillage has no page at <code>{path}</code>.</p>
<p><a href="/">Search for a task or a page</a></p>
