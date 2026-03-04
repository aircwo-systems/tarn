<script lang="ts">
	export let connGateways;
	export let connQueues;
	export let connBuckets;
	export let connFunctions;
	export let connSecrets;
	export let apigwToQueueEdges;
	export let apigwToFnEdges;
	export let queueToFnEdges;
	export let s3ToFnEdges;
	export let fnToCacheEdges;
	export let cacheToSecretEdges;
	export let fnToInfraEdges;
	export let connCacheExtension;
	export let connInfraNodes;
	export let connInfraLane;
	export let connInfraRouteX;
	export let connInfraRouteY;
	export let infra;
	export let infraNodeHalfWidth;
	export let connNodeHalfWidth;
	export let cacheNodeHalfWidth;
	export let CW;
	export let CH;
	export let openGateway;
	export let handleGatewayKeydown;
	export let canvasExpanded;
	export let hasData;
	export let connGatewayX;
	export let connQueueX;
	export let connBucketX;
	export let connFunctionX;
	export let connSecretX;
</script>

<svg
	viewBox={`0 0 ${CW} ${CH}`}
	class="w-full"
	style={`min-width: ${canvasExpanded ? 980 : 620}px; max-height: ${canvasExpanded ? 760 : 460}px;`}
>
	{#each Array(Math.floor(CW / 48)) as _, ix}
		{#each Array(Math.floor(CH / 48)) as _, iy}
			<circle cx={24 + ix * 48} cy={24 + iy * 48} r="0.55" class="fill-border" opacity="0.45" />
		{/each}
	{/each}

	{#if hasData}
		<text x={connGatewayX} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">
			Gateways
		</text>
		<text x={connQueueX} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">
			Queues
		</text>
		<text x={connBucketX} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">
			Buckets
		</text>
		<text x={connFunctionX} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">
			Functions
		</text>
		<text x={connSecretX} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">
			Secrets
		</text>

		<!-- Edges and nodes rendering logic should be added here, using the props -->
		<!-- ...existing code from topology-canvas.svelte for connections view... -->

	{:else}
		<text x={CW / 2} y={CH / 2} text-anchor="middle" class="fill-text-faint" font-size="11" font-family="var(--font-mono)">
			Connecting...
		</text>
	{/if}
</svg>
