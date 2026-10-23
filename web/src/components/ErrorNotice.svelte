<!-- A failed request, in plain words. -->
<script lang="ts">
	import { errorText } from '../lib/errorText';

	let { error, notFound }: { error: unknown; notFound: string } = $props();
</script>

<p role="alert">{errorText(error, notFound)}</p>
