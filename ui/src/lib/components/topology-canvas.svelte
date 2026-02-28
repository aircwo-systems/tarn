<script lang="ts">
	import GatewayDetailsPanel from '$lib/components/gateway-details-panel.svelte';
	import { getDashboard, getDashboardFilters, matchesTagFilter } from '$lib/state.svelte';

	import type { InfraConnection, InfraProbe } from '$lib/types';

	type NodeKind = 'gateway' | 'queue' | 'function' | 'secret' | 'extension' | 'infra';
	type ConnectionNode = {
		id: string;
		x: number;
		y: number;
		label: string;
		sub: string;
		kind: NodeKind;
	};

	const dashboard = getDashboard();
	const filters = getDashboardFilters();

	const gateways = $derived((dashboard.data?.gateways ?? []).filter((gateway) => matchesTagFilter(gateway.tags, filters.tagFilter)));
	const functions = $derived((dashboard.data?.functions ?? []).filter((fn) => matchesTagFilter(fn.tags, filters.tagFilter)));
	const queues = $derived((dashboard.data?.queues ?? []).filter((queue) => matchesTagFilter(queue.tags, filters.tagFilter)));
	const secrets = $derived((dashboard.data?.secrets ?? []).filter((secret) => matchesTagFilter(secret.tags, filters.tagFilter)));
	const infra = $derived(dashboard.data?.infrastructure ?? []);
	const infraConnections = $derived(dashboard.data?.connections ?? []);
	const hasData = $derived(!!dashboard.data);

	let viewMode = $state<'components' | 'connections'>('components');
	let selectedGatewayId = $state('');

	// Component view geometry
	const W = 840;
	const H = 380;
	const CX = W / 2;
	const endpoint = { x: CX, y: 52 };
	const laneY = 150;
	const serviceX = { apigw: 90, lambda: 240, sqs: 400, secrets: 560, infra: W - 90 };

	function fanPositions(cx: number, count: number, baseY: number, spacing: number): { x: number; y: number }[] {
		if (count === 0) return [];
		const totalWidth = (count - 1) * spacing;
		const startX = cx - totalWidth / 2;
		return Array.from({ length: count }, (_, i) => ({
			x: startX + i * spacing,
			y: baseY
		}));
	}

	const gwPositions = $derived(fanPositions(serviceX.apigw, Math.min(gateways.length, 6), laneY + 90, 40));
	const fnPositions = $derived(fanPositions(serviceX.lambda, Math.min(functions.length, 6), laneY + 90, 48));
	const qPositions = $derived(fanPositions(serviceX.sqs, Math.min(queues.length, 6), laneY + 90, 48));
	const sPositions = $derived(fanPositions(serviceX.secrets, Math.min(secrets.length, 6), laneY + 90, 48));
	const infraPositions = $derived(fanPositions(serviceX.infra, Math.min(infra.length, 6), laneY + 90, 40));

	// Connection view geometry
	const CW = 960;
	const CH = 500;
	const connEndpoint = { x: CW / 2, y: 42 };
	const connGatewayX = 80;
	const connQueueX = 240;
	const connFunctionX = 400;
	const connSecretX = 700;
	const connInfraLane = {
		x: CW - 150,
		y: 150,
		width: 124,
		height: 280
	};
	const connInfraX = connInfraLane.x + connInfraLane.width / 2;
	const connInfraRouteX = connInfraLane.x - 26;
	const connInfraRouteY = connInfraLane.y + connInfraLane.height + 18;
	const connServices = {
		apigw: { x: connGatewayX, y: 92, label: 'API Gateway' },
		sqs: { x: connQueueX, y: 92, label: 'SQS Service' },
		lambda: { x: connFunctionX, y: 92, label: 'Lambda Service' },
		secrets: { x: connSecretX, y: 92, label: 'Secrets Service' }
	};
	const connNodeHalfWidth = 54;
	const cacheNodeHalfWidth = 50;
	const infraNodeHalfWidth = 54;
	const minConnectionSpacing = 28;

	function trimLabel(label: string, max = 14): string {
		if (label.length <= max) return label;
		return `${label.slice(0, max - 1)}…`;
	}

	const connGateways = $derived(
		gateways.slice(0, 3).map(
			(gw, i): ConnectionNode => ({
				id: gw.apiId,
				x: connGatewayX,
				y: 182 + i * 54,
				label: trimLabel(gw.name, 13),
				sub: `${gw.routes} routes`,
				kind: 'gateway'
			})
		)
	);

	const connQueues = $derived(
		queues.slice(0, 4).map(
			(q, i): ConnectionNode => ({
				id: q.name,
				x: connQueueX,
				y: 168 + i * 52,
				label: trimLabel(q.name, 13),
				sub: `${q.approxVisible + q.approxInFlight + q.approxDelayed} msg`,
				kind: 'queue'
			})
		)
	);

	const connFunctions = $derived(
		functions.slice(0, 4).map(
			(fn, i): ConnectionNode => ({
				id: fn.name,
				x: connFunctionX,
				y: 168 + i * 52,
				label: trimLabel(fn.name, 13),
				sub: fn.runtime,
				kind: 'function'
			})
		)
	);

	const connSecrets = $derived(
		secrets.slice(0, 3).map(
			(s, i): ConnectionNode => ({
				id: s.name,
				x: connSecretX,
				y: 180 + i * 58,
				label: trimLabel(s.name, 13),
				sub: `v${s.versionId.slice(0, 6)}`,
				kind: 'secret'
			})
		)
	);

	function infraStatusLabel(probe: InfraProbe): string {
		if (probe.status === 'connected') {
			return probe.version ? `v${probe.version.split('.').slice(0, 2).join('.')}` : `${Math.round(probe.latencyMs)}ms`;
		}
		return probe.status;
	}

	const connInfraNodes = $derived(
		infra.slice(0, 4).map(
			(probe, i): ConnectionNode => ({
				id: `${probe.kind}-${probe.host}-${probe.port}`,
				x: connInfraX,
				y: connInfraLane.y + 62 + i * 58,
				label: trimLabel(probe.name, 13),
				sub: probe.port > 0 ? `:${probe.port}` : infraStatusLabel(probe),
				kind: 'infra'
			})
		)
	);

	const fnToInfraEdges = $derived(
		connInfraNodes.length === 0 || connFunctions.length === 0 || infraConnections.length === 0
			? []
			: infraConnections.flatMap((connection): Array<{ from: ConnectionNode; to: ConnectionNode; evidence: InfraConnection['evidence'] }> => {
					const from = connFunctions.find((node) => node.id === connection.sourceFunction);
					const to = connInfraNodes.find((node) => node.id === connection.targetId);
					if (!from || !to) {
						return [];
					}
					return [{ from, to, evidence: connection.evidence }];
				})
	);

	const lambdaColumnRightX = connFunctionX + connNodeHalfWidth;
	const secretColumnLeftX = connSecretX - connNodeHalfWidth;
	const cacheMinX = lambdaColumnRightX + cacheNodeHalfWidth + minConnectionSpacing;
	const cacheMaxX = secretColumnLeftX - cacheNodeHalfWidth - minConnectionSpacing;
	const cacheTargetX = Math.round(lambdaColumnRightX + (secretColumnLeftX - lambdaColumnRightX) * 0.68);

	const connCacheExtension = $derived<ConnectionNode>({
		id: 'secrets-cache-extension',
		x: cacheMinX <= cacheMaxX ? Math.max(cacheMinX, Math.min(cacheMaxX, cacheTargetX)) : cacheTargetX,
		y: CH / 2 + 10,
		label: 'Secrets Cache Ext',
		sub: 'localhost:2773',
		kind: 'extension'
	});

	const gatewayToQueueEdges = $derived(
		connQueues.length === 0
			? []
			: connGateways.map((gw, i) => ({
					from: gw,
					to: connQueues[i % connQueues.length]
				}))
	);

	const queueToFnEdges = $derived(
		connFunctions.length === 0
			? []
			: connQueues.map((q, i) => ({
					from: q,
					to: connFunctions[i % connFunctions.length]
				}))
	);

	const fnToCacheEdges = $derived(
		connFunctions.map((fn) => ({
			from: fn,
			to: connCacheExtension
		}))
	);

	const cacheToSecretEdges = $derived(
		connSecrets.map((secret) => ({
			from: connCacheExtension,
			to: secret
		}))
	);

	const connectionCount = $derived(gatewayToQueueEdges.length + queueToFnEdges.length + fnToCacheEdges.length + cacheToSecretEdges.length + fnToInfraEdges.length);
	const selectedGateway = $derived(gateways.find((gateway) => gateway.apiId === selectedGatewayId) ?? null);

	$effect(() => {
		if (selectedGatewayId && !gateways.some((gateway) => gateway.apiId === selectedGatewayId)) {
			selectedGatewayId = '';
		}
	});

	function curvePath(from: { x: number; y: number }, to: { x: number; y: number }): string {
		const c1x = from.x + (to.x - from.x) * 0.35;
		const c2x = from.x + (to.x - from.x) * 0.65;
		return `M ${from.x} ${from.y} C ${c1x} ${from.y}, ${c2x} ${to.y}, ${to.x} ${to.y}`;
	}

	function roundedPath(points: Array<{ x: number; y: number }>, radius: number): string {
		if (points.length === 0) return '';
		if (points.length === 1) return `M ${points[0].x} ${points[0].y}`;

		const commands = [`M ${points[0].x} ${points[0].y}`];

		for (let i = 1; i < points.length - 1; i += 1) {
			const prev = points[i - 1];
			const curr = points[i];
			const next = points[i + 1];

			const inDx = curr.x - prev.x;
			const inDy = curr.y - prev.y;
			const outDx = next.x - curr.x;
			const outDy = next.y - curr.y;
			const inLen = Math.hypot(inDx, inDy);
			const outLen = Math.hypot(outDx, outDy);

			if (inLen === 0 || outLen === 0) {
				continue;
			}

			const cornerRadius = Math.min(radius, inLen / 2, outLen / 2);
			const inX = curr.x - (inDx / inLen) * cornerRadius;
			const inY = curr.y - (inDy / inLen) * cornerRadius;
			const outX = curr.x + (outDx / outLen) * cornerRadius;
			const outY = curr.y + (outDy / outLen) * cornerRadius;

			commands.push(`L ${inX} ${inY}`);
			commands.push(`Q ${curr.x} ${curr.y} ${outX} ${outY}`);
		}

		const last = points[points.length - 1];
		commands.push(`L ${last.x} ${last.y}`);
		return commands.join(' ');
	}

	function infraLadderPath(from: ConnectionNode, to: ConnectionNode): string {
		const startX = from.x + connNodeHalfWidth;
		const endX = to.x - infraNodeHalfWidth;
		const routeY = Math.max(connInfraRouteY, from.y + 44);
		return roundedPath(
			[
				{ x: startX, y: from.y },
				{ x: startX + 28, y: from.y },
				{ x: startX + 82, y: routeY },
				{ x: connInfraRouteX, y: routeY },
				{ x: endX - 20, y: to.y },
				{ x: endX, y: to.y }
			],
			18
		);
	}

	function stateColor(state: string): 'green' | 'amber' | 'red' | 'gray' {
		const s = state.toLowerCase();
		if (s === 'active' || s === 'running') return 'green';
		if (s === 'pending') return 'amber';
		if (s === 'failed' || s === 'inactive') return 'red';
		return 'gray';
	}

	const ledColorMap: Record<string, string> = {
		green: 'var(--color-accent)',
		amber: 'var(--color-amber)',
		red: 'var(--color-red)',
		gray: 'var(--color-text-faint)'
	};

	function openGateway(apiId: string) {
		selectedGatewayId = apiId;
	}

	function closeGatewayPanel() {
		selectedGatewayId = '';
	}

	function handleGatewayKeydown(event: KeyboardEvent, apiId: string) {
		if (event.key === 'Enter' || event.key === ' ') {
			event.preventDefault();
			openGateway(apiId);
		}
	}
</script>

<div class="rounded-lg border border-border bg-bg-raised overflow-hidden">
	<div class="flex items-center justify-between px-3 py-2 border-b border-border gap-3">
		<h3 class="text-xs font-mono uppercase tracking-wider text-text-muted">Topology</h3>

		<div class="inline-flex rounded-md border border-border bg-bg-surface p-0.5">
			<button
				type="button"
				class={`px-2.5 py-1 text-[10px] font-mono uppercase tracking-wide rounded-sm transition ${
					viewMode === 'components'
						? 'bg-bg-overlay text-text'
						: 'text-text-faint hover:text-text-muted'
				}`}
				onclick={() => (viewMode = 'components')}
			>
				Component View
			</button>
			<button
				type="button"
				class={`px-2.5 py-1 text-[10px] font-mono uppercase tracking-wide rounded-sm transition ${
					viewMode === 'connections'
						? 'bg-bg-overlay text-text'
						: 'text-text-faint hover:text-text-muted'
				}`}
				onclick={() => (viewMode = 'connections')}
			>
				Connection View
			</button>
		</div>

		<span class="text-[10px] text-text-faint font-mono">
			{viewMode === 'components'
				? `${gateways.length + functions.length + queues.length + secrets.length + infra.length} resources`
				: `${connectionCount} links`}
		</span>
	</div>

	<div class="flex flex-col lg:flex-row">
		<div class="relative min-w-0 flex-1 overflow-x-auto">
			{#if viewMode === 'components'}
				<svg viewBox="0 0 {W} {H}" class="w-full min-w-[480px]" style="max-height: 380px;">
				{#each Array(Math.floor(W / 40)) as _, ix}
					{#each Array(Math.floor(H / 40)) as _, iy}
						<circle cx={20 + ix * 40} cy={20 + iy * 40} r="0.6" class="fill-border" opacity="0.5" />
					{/each}
				{/each}

				{#if hasData}
					<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.apigw} y2={laneY - 16}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
					<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.lambda} y2={laneY - 16}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
					<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.sqs} y2={laneY - 16}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
					<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.secrets} y2={laneY - 16}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
					<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.infra} y2={laneY - 16}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />

					{#each gwPositions as pos}
						<line x1={serviceX.apigw} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
							stroke="var(--color-red)" stroke-width="0.75" opacity="0.35" />
					{/each}
					{#each fnPositions as pos}
						<line x1={serviceX.lambda} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
							stroke="var(--color-accent)" stroke-width="0.75" opacity="0.3" />
					{/each}
					{#each qPositions as pos}
						<line x1={serviceX.sqs} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
							stroke="var(--color-amber)" stroke-width="0.75" opacity="0.3" />
					{/each}
					{#each sPositions as pos}
						<line x1={serviceX.secrets} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
							stroke="var(--color-blue)" stroke-width="0.75" opacity="0.3" />
					{/each}
					{#each infraPositions as pos}
						<line x1={serviceX.infra} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
							stroke="var(--color-purple, var(--color-text-faint))" stroke-width="0.75" opacity="0.3" />
					{/each}

					<g>
						<rect x={endpoint.x - 56} y={endpoint.y - 16} width="112" height="32" rx="6"
							class="fill-bg-surface stroke-border-strong" stroke-width="1" />
						<circle cx={endpoint.x - 38} cy={endpoint.y} r="3" class="fill-accent" opacity="0.8">
							<animate attributeName="opacity" values="0.8;0.4;0.8" dur="2s" repeatCount="indefinite" />
						</circle>
						<text x={endpoint.x - 26} y={endpoint.y + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">
							OpenStack
						</text>
					</g>

					<g>
						<rect x={serviceX.apigw - 52} y={laneY - 14} width="104" height="28" rx="5"
							class="fill-bg-surface" stroke="var(--color-red)" stroke-width="1" opacity="0.8" />
						<text x={serviceX.apigw - 30} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">
							APIGW
						</text>
					</g>

					<g>
						<rect x={serviceX.lambda - 44} y={laneY - 14} width="88" height="28" rx="5"
							class="fill-bg-surface" stroke="var(--color-accent)" stroke-width="1" opacity="0.8" />
						<text x={serviceX.lambda - 20} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">
							Lambda
						</text>
					</g>

					<g>
						<rect x={serviceX.sqs - 36} y={laneY - 14} width="72" height="28" rx="5"
							class="fill-bg-surface" stroke="var(--color-amber)" stroke-width="1" opacity="0.8" />
						<text x={serviceX.sqs - 12} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">
							SQS
						</text>
					</g>

					<g>
						<rect x={serviceX.secrets - 44} y={laneY - 14} width="88" height="28" rx="5"
							class="fill-bg-surface" stroke="var(--color-blue)" stroke-width="1" opacity="0.8" />
						<text x={serviceX.secrets - 20} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">
							Secrets
						</text>
					</g>

					<g>
						<rect x={serviceX.infra - 44} y={laneY - 14} width="88" height="28" rx="5"
							class="fill-bg-surface stroke-text-faint" stroke-width="1" opacity="0.8" />
						<text x={serviceX.infra - 16} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">
							Infra
						</text>
					</g>

					{#each gwPositions as pos, i}
						{@const gw = gateways[i]}
						<g
							role="button"
							tabindex="0"
							aria-label={`Open API Gateway details for ${gw.name}`}
							class="cursor-pointer"
							onclick={() => openGateway(gw.apiId)}
							onkeydown={(event: KeyboardEvent) => handleGatewayKeydown(event, gw.apiId)}
						>
							<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
								class="fill-bg-overlay" stroke="var(--color-red)" stroke-width="0.75" opacity="0.7" />
							<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-red)" opacity="0.78" />
							<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">
								{gw.name.length > 4 ? gw.name.slice(0, 4) : gw.name}
							</text>
						</g>
					{/each}

					{#each fnPositions as pos, i}
						{@const fn = functions[i]}
						<g>
							<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
								class="fill-bg-overlay" stroke={ledColorMap[stateColor(fn.state)]} stroke-width="0.75" />
							<circle cx={pos.x - 9} cy={pos.y} r="2" fill={ledColorMap[stateColor(fn.state)]} />
							<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">
								{fn.name.length > 4 ? fn.name.slice(0, 4) : fn.name}
							</text>
						</g>
					{/each}

					{#each qPositions as pos, i}
						{@const q = queues[i]}
						<g>
							<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
								class="fill-bg-overlay" stroke="var(--color-amber)" stroke-width="0.75" opacity="0.6" />
							<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-amber)" opacity="0.7" />
							<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">
								{q.name.length > 4 ? q.name.slice(0, 4) : q.name}
							</text>
						</g>
					{/each}

					{#each sPositions as pos, i}
						{@const s = secrets[i]}
						<g>
							<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
								class="fill-bg-overlay" stroke="var(--color-blue)" stroke-width="0.75" opacity="0.6" />
							<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-blue)" opacity="0.7" />
							<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">
								{s.name.length > 4 ? s.name.slice(0, 4) : s.name}
							</text>
						</g>
					{/each}

					{#each infraPositions as pos, i}
						{@const probe = infra[i]}
						<g>
							<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
								class="fill-bg-overlay" stroke={probe.status === 'connected' ? 'var(--color-accent)' : 'var(--color-red)'} stroke-width="0.75" opacity="0.7" />
							<circle cx={pos.x - 9} cy={pos.y} r="2" fill={probe.status === 'connected' ? 'var(--color-accent)' : 'var(--color-red)'} opacity="0.78" />
							<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">
								{probe.name.length > 4 ? probe.name.slice(0, 4) : probe.name}
							</text>
						</g>
					{/each}

				{:else}
					<text x={CX} y={H / 2} text-anchor="middle" class="fill-text-faint" font-size="11" font-family="var(--font-mono)">
						{dashboard.loading ? 'Connecting...' : 'No data'}
					</text>
				{/if}
				</svg>
			{:else}
				<svg viewBox="0 0 {CW} {CH}" class="w-full min-w-[620px]" style="max-height: 460px;">
				{#each Array(Math.floor(CW / 48)) as _, ix}
					{#each Array(Math.floor(CH / 48)) as _, iy}
						<circle cx={24 + ix * 48} cy={24 + iy * 48} r="0.55" class="fill-border" opacity="0.45" />
					{/each}
				{/each}

				{#if hasData}
					<line x1={connEndpoint.x} y1={connEndpoint.y + 14} x2={connServices.apigw.x} y2={connServices.apigw.y - 14}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
					<line x1={connEndpoint.x} y1={connEndpoint.y + 14} x2={connServices.sqs.x} y2={connServices.sqs.y - 14}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
					<line x1={connEndpoint.x} y1={connEndpoint.y + 14} x2={connServices.lambda.x} y2={connServices.lambda.y - 14}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
					<line x1={connEndpoint.x} y1={connEndpoint.y + 14} x2={connServices.secrets.x} y2={connServices.secrets.y - 14}
						stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />

					{#each gatewayToQueueEdges as edge}
						<path d={curvePath({ x: edge.from.x + connNodeHalfWidth, y: edge.from.y }, { x: edge.to.x - connNodeHalfWidth, y: edge.to.y })}
							stroke="var(--color-red)" stroke-width="1.2" fill="none" opacity="0.58" />
					{/each}

					{#each queueToFnEdges as edge}
						<path d={curvePath({ x: edge.from.x + connNodeHalfWidth, y: edge.from.y }, { x: edge.to.x - connNodeHalfWidth, y: edge.to.y })}
							stroke="var(--color-amber)" stroke-width="1.2" fill="none" opacity="0.55" />
					{/each}

					<line
						x1={connServices.secrets.x}
						y1={connServices.secrets.y + 14}
						x2={connCacheExtension.x}
						y2={connCacheExtension.y - 20}
						stroke="var(--color-border-strong)"
						stroke-width="1"
						stroke-dasharray="4 3"
					/>

					{#each fnToCacheEdges as edge}
						<path d={curvePath({ x: edge.from.x + connNodeHalfWidth, y: edge.from.y }, { x: edge.to.x - cacheNodeHalfWidth, y: edge.to.y })}
							stroke="var(--color-blue)" stroke-width="1.1" fill="none" opacity="0.52" stroke-dasharray="5 4" />
					{/each}

					{#each cacheToSecretEdges as edge}
						<path d={curvePath({ x: edge.from.x + cacheNodeHalfWidth, y: edge.from.y }, { x: edge.to.x - connNodeHalfWidth, y: edge.to.y })}
							stroke="var(--color-blue)" stroke-width="1.2" fill="none" opacity="0.62" />
					{/each}

					<g>
						<rect x={connEndpoint.x - 58} y={connEndpoint.y - 13} width="116" height="26" rx="6"
							class="fill-bg-surface stroke-border-strong" stroke-width="1" />
						<text x={connEndpoint.x} y={connEndpoint.y + 3.5} text-anchor="middle" class="fill-text" font-size="9" font-family="var(--font-mono)">
							OpenStack Endpoint
						</text>
					</g>

					{#each Object.values(connServices) as service}
						<g>
							<rect x={service.x - 54} y={service.y - 13} width="108" height="26" rx="6"
								class="fill-bg-surface stroke-border" stroke-width="1" />
							<text x={service.x} y={service.y + 3.5} text-anchor="middle" class="fill-text-muted" font-size="8.5" font-family="var(--font-mono)">
								{service.label}
							</text>
						</g>
					{/each}

					{#each connGateways as node}
						<g
							role="button"
							tabindex="0"
							aria-label={`Open API Gateway details for ${node.label}`}
							class="cursor-pointer"
							onclick={() => openGateway(node.id)}
							onkeydown={(event: KeyboardEvent) => handleGatewayKeydown(event, node.id)}
						>
							<rect x={node.x - connNodeHalfWidth} y={node.y - 16} width={connNodeHalfWidth * 2} height="32" rx="6"
								class="fill-bg-overlay" stroke="var(--color-red)" stroke-width="1" opacity="0.84" />
							<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">
								{node.label}
							</text>
							<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">
								{node.sub}
							</text>
						</g>
					{/each}

					{#each connQueues as node}
						<g>
							<rect x={node.x - connNodeHalfWidth} y={node.y - 16} width={connNodeHalfWidth * 2} height="32" rx="6"
								class="fill-bg-overlay" stroke="var(--color-amber)" stroke-width="1" opacity="0.8" />
							<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">
								{node.label}
							</text>
							<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">
								{node.sub}
							</text>
						</g>
					{/each}

					{#each connFunctions as node}
						<g>
							<rect x={node.x - connNodeHalfWidth} y={node.y - 16} width={connNodeHalfWidth * 2} height="32" rx="6"
								class="fill-bg-overlay" stroke="var(--color-accent)" stroke-width="1" opacity="0.82" />
							<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">
								{node.label}
							</text>
							<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">
								{node.sub}
							</text>
						</g>
					{/each}

					<g>
						<rect x={connCacheExtension.x - cacheNodeHalfWidth} y={connCacheExtension.y - 18} width={cacheNodeHalfWidth * 2} height="36" rx="7"
							class="fill-bg-overlay" stroke="var(--color-blue)" stroke-width="1.1" opacity="0.86" />
						<text x={connCacheExtension.x} y={connCacheExtension.y - 3} text-anchor="middle" class="fill-text" font-size="8.4" font-family="var(--font-mono)">
							{connCacheExtension.label}
						</text>
						<text x={connCacheExtension.x} y={connCacheExtension.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">
							{connCacheExtension.sub}
						</text>
					</g>

					{#each connSecrets as node}
						<g>
							<rect x={node.x - connNodeHalfWidth} y={node.y - 16} width={connNodeHalfWidth * 2} height="32" rx="6"
								class="fill-bg-overlay" stroke="var(--color-blue)" stroke-width="1" opacity="0.82" />
							<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">
								{node.label}
							</text>
							<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">
								{node.sub}
							</text>
						</g>
					{/each}

					<!-- Infrastructure ladder -->
					{#if infra.length > 0}
						<g>
							<rect
								x={connInfraLane.x}
								y={connInfraLane.y}
								width={connInfraLane.width}
								height={connInfraLane.height}
								rx="12"
								class="fill-bg-surface stroke-border"
								stroke-width="1"
								opacity="0.42"
							/>
							<text
								x={connInfraLane.x + 18}
								y={connInfraLane.y + 18}
								class="fill-text-muted"
								font-size="8.5"
								font-family="var(--font-mono)"
							>
								Infrastructure Ladder
							</text>
							<text
								x={connInfraLane.x + 18}
								y={connInfraLane.y + 32}
								class="fill-text-faint"
								font-size="7"
								font-family="var(--font-mono)"
							>
								Observed targets stacked for clean routing
							</text>
						</g>

						{#each fnToInfraEdges as edge}
							<path
								d={infraLadderPath(edge.from, edge.to)}
								stroke="var(--color-accent)"
								stroke-width="1.1"
								fill="none"
								opacity="0.45"
								stroke-dasharray="5 4"
							/>
						{/each}

						{#each connInfraNodes as node, i}
							{@const probe = infra[i]}
							<g>
								<rect x={node.x - infraNodeHalfWidth} y={node.y - 16} width={infraNodeHalfWidth * 2} height="32" rx="6"
									class="fill-bg-overlay" stroke={probe.status === 'connected' ? 'var(--color-accent)' : 'var(--color-red)'} stroke-width="1" opacity="0.82" />
								<circle cx={node.x - infraNodeHalfWidth + 12} cy={node.y} r="3"
									fill={probe.status === 'connected' ? 'var(--color-accent)' : 'var(--color-red)'}
									opacity="0.8">
									{#if probe.status === 'connected'}
										<animate attributeName="opacity" values="0.8;0.4;0.8" dur="3s" repeatCount="indefinite" />
									{/if}
								</circle>
								<text x={node.x + 4} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">
									{node.label}
								</text>
								<text x={node.x + 4} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">
									{node.sub}
								</text>
							</g>
						{/each}
					{/if}

						<text x={CW / 2} y={CH - 16} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">
							Rose: APIGW · Amber: queues · Green dashed: lambda→infra ladder · Blue: secrets cache · Blue solid: cache→secrets
						</text>
				{:else}
					<text x={CW / 2} y={CH / 2} text-anchor="middle" class="fill-text-faint" font-size="11" font-family="var(--font-mono)">
						{dashboard.loading ? 'Connecting...' : 'No data'}
					</text>
				{/if}
				</svg>
			{/if}
		</div>

		{#if selectedGateway}
			<div class="w-full border-t border-border bg-bg-surface/40 p-3 lg:w-[22rem] lg:border-l lg:border-t-0">
				<GatewayDetailsPanel gateway={selectedGateway} onClose={closeGatewayPanel} />
			</div>
		{/if}
	</div>
</div>
