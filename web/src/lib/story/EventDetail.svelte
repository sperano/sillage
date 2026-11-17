<script lang="ts">
	import type { Deliveries } from '../decodeApi';
	import { isStateChange } from '../deliveries';
	import type { Load } from '../load.svelte';
	import { eventTime, type Event, type Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import type { TimelineItem } from '../timeline';
	import { eventRef } from '../timeline';
	import { eventLink, type LinkConfig } from '../links';
	import { accountLabel, eventLabel } from '../describe';
	import { attributionUncertain } from '../attribution';
	import Correlations from './Correlations.svelte';
	import EvidenceList from './EvidenceList.svelte';
	import NearbyDeliveries from './NearbyDeliveries.svelte';
	import SourceLinkView from './SourceLinkView.svelte';
	import Time from './Time.svelte';

	let {
		story,
		items,
		selection,
		event,
		links,
		timeZone,
		deliveries = { state: 'idle' }
	}: {
		story: Story;
		items: TimelineItem[];
		selection: Selection;
		event: Event;
		links: LinkConfig;
		timeZone: string;
		/** The root's webhook deliveries, listed around a state change. */
		deliveries?: Load<Deliveries>;
	} = $props();

	const uncertain = $derived(attributionUncertain(story, event));
	const details = $derived(Object.entries(event.details ?? {}));
</script>

<h3 tabindex="-1">{eventLabel(event)}</h3>
<dl>
	<dt>Kind</dt>
	<dd><code>{event.kind}</code></dd>
	<dt>Source time</dt>
	<dd data-testid="source-time">
		{#if event.sourceTime}
			<Time at={event.sourceTime} {timeZone} />
		{:else}
			none: a state seen by polling, not when it changed
		{/if}
	</dd>
	<dt>Observed</dt>
	<dd><Time at={event.observedAt} {timeZone} /></dd>
	{#if event.lastObservedAt}
		<dt>Last observed</dt>
		<dd data-testid="last-observed">
			<Time at={event.lastObservedAt} {timeZone} />: seen unchanged by every read until then; when
			it changed is unknown
		</dd>
	{/if}
	<dt>Account</dt>
	<dd>{accountLabel(event.actor)}</dd>
	<dt>Executor</dt>
	<dd>
		{#if event.executor}
			{event.executor.name} ({event.executor.kind})
		{:else}
			not known
		{/if}
	</dd>
	{#if uncertain}
		<dt>Attribution</dt>
		<dd>uncertain: {uncertain}</dd>
	{/if}
	{#each details as [k, v] (k)}
		<dt>{k}</dt>
		<dd>{v}</dd>
	{/each}
	<dt>Source record</dt>
	<dd><SourceLinkView link={eventLink(story, event, links)} /></dd>
	<dt>Id</dt>
	<dd>
		<code>{event.id}</code>
		{#if event.idKind === 'fingerprint'}(a fingerprint: the source has no id for it){/if}
	</dd>
</dl>
<h4>Evidence</h4>
<EvidenceList evidence={[event.evidence]} />
<Correlations {story} {items} {selection} subject={eventRef(event)} />
{#if isStateChange(event)}
	<NearbyDeliveries {deliveries} at={eventTime(event)} {selection} {timeZone} />
{/if}
