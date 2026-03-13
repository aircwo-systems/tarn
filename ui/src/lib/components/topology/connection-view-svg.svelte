<script lang="ts">
	let {
		connGateways,
		connQueues,
		connBuckets,
		connFunctions,
		connSecrets,
		apigwToQueueEdges,
		apigwToFnEdges,
		queueToFnEdges,
		s3ToFnEdges,
		fnToCacheEdges,
		cacheToSecretEdges,
		fnToInfraEdges,
		connCacheExtension,
		connInfraNodes,
		connInfraLane,
		connInfraRouteX,
		connInfraRouteY,
		infra,
		infraNodeHalfWidth,
		connNodeHalfWidth,
		cacheNodeHalfWidth,
		CW,
		CH,
		openGateway,
		handleGatewayKeydown,
		canvasExpanded,
		hasData,
		connGatewayX,
		connQueueX,
		connBucketX,
		connFunctionX,
		connSecretX,
	}: any = $props();
</script>

<svg
	viewBox={`0 0 ${CW} ${CH}`}
	class="w-full"
	style={`min-width: ${canvasExpanded ? 980 : 620}px; max-height: ${canvasExpanded ? 760 : 460}px;`}
>
	{#each Array(Math.floor(CW / 48)) as _, ix}
		{#each Array(Math.floor(CH / 48)) as _, iy}
			<circle cx={24 + ix * 48} cy={24 + iy * 48} r="0.55" class="fill-[var(--color-border)]" opacity="0.45" />
		{/each}
	{/each}

	{#if hasData}
		<text x={connGatewayX} y={92} text-anchor="middle" class="fill-foreground-faint" font-size="8" font-family="var(--font-mono)">
			Gateways
		</text>
		<text x={connQueueX} y={92} text-anchor="middle" class="fill-foreground-faint" font-size="8" font-family="var(--font-mono)">
			Queues
		</text>
		<text x={connBucketX} y={92} text-anchor="middle" class="fill-foreground-faint" font-size="8" font-family="var(--font-mono)">
			Buckets
		</text>
		<text x={connFunctionX} y={92} text-anchor="middle" class="fill-foreground-faint" font-size="8" font-family="var(--font-mono)">
			Functions
		</text>
		<text x={connSecretX} y={92} text-anchor="middle" class="fill-foreground-faint" font-size="8" font-family="var(--font-mono)">
			Secrets
		</text>

		<!-- Edges and nodes rendering logic should be added here, using the props -->
		<!-- ...existing code from topology-canvas.svelte for connections view... -->

	{:else}
		<text x={CW / 2} y={CH / 2} text-anchor="middle" class="fill-foreground-faint" font-size="11" font-family="var(--font-mono)">
			Connecting...
		</text>
	{/if}
</svg>
