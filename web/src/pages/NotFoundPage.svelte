<!-- Any other path. -->
<script lang="ts">
	let { path }: { path: string } = $props();
</script>

<h1>Not found</h1>
<p>Sillage has no page at <code>{path}</code>.</p>
<p><a href="/">Search for a task or a page</a></p>
