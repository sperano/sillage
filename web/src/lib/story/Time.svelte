<script lang="ts">
	import { formatTime } from '../time';

	let { at, timeZone }: { at: string; timeZone: string } = $props();
</script>

<time datetime={at} title={at}>{formatTime(at, timeZone)}</time>
