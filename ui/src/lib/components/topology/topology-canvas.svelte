<script lang="ts">
	import { ArrowsInSimpleIcon, ArrowsOutSimpleIcon, CaretDownIcon, CaretUpIcon } from 'phosphor-svelte';
	import GatewayDetailsPanel from '$lib/components/topology/gateway-details-panel.svelte';
	import { getDashboard, getDashboardFilters, matchesTagFilter } from '$lib/state.svelte';

	import type { InfraConnection, InfraProbe } from '$lib/types';

	type NodeKind = 'gateway' | 'queue' | 'bucket' | 'function' | 'secret' | 'extension' | 'infra';
	type ConnectionNode = {
		id: string;
		x: number;
		y: number;
		label: string;
		sub: string;
		kind: NodeKind;
		status?: string;
	};

	const dashboard = getDashboard();
	const filters = getDashboardFilters();

	const gateways = $derived((dashboard.data?.gateways ?? []).filter((gateway) => matchesTagFilter(gateway.tags, filters.tagFilter)));
	const functions = $derived((dashboard.data?.functions ?? []).filter((fn) => matchesTagFilter(fn.tags, filters.tagFilter)));
	const queues = $derived((dashboard.data?.queues ?? []).filter((queue) => matchesTagFilter(queue.tags, filters.tagFilter)));
	const secrets = $derived((dashboard.data?.secrets ?? []).filter((secret) => matchesTagFilter(secret.tags, filters.tagFilter)));
	const buckets = $derived(dashboard.data?.buckets ?? []);
	const eventSourceMappings = $derived(dashboard.data?.eventSourceMappings ?? []);
	const infra = $derived(dashboard.data?.infrastructure ?? []);
	const infraConnections = $derived(dashboard.data?.connections ?? []);
	const hasData = $derived(!!dashboard.data);

	let viewMode = $state<'components' | 'connections'>('components');
	let selectedGatewayId = $state('');
	let canvasExpanded = $state(false);
	let infraOrderIds = $state<string[]>([]);
	let infraOrderHydrated = $state(false);
	const INFRA_ORDER_STORAGE_KEY = 'openstack-ui-topology-infra-order-v1';

	// Component view geometry
	const W = 960;
	const H = 380;
	const CX = W / 2;
	const endpoint = { x: CX, y: 52 };
	const laneY = 150;
	const serviceX = { apigw: 80, lambda: 220, sqs: 370, secrets: 520, s3: 670, infra: W - 70 };

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
	const s3Positions = $derived(fanPositions(serviceX.s3, Math.min(buckets.length, 6), laneY + 90, 48));
	const infraPositions = $derived(fanPositions(serviceX.infra, Math.min(infra.length, 6), laneY + 90, 40));

	// Connection view geometry
	const CW = 960;
	const CH = 500;
	const connGatewayX = 88;
	const connQueueX = 234;
	const connBucketX = 790;
	const connFunctionX = 470;
	const connSecretX = 720;
	const connInfraX = CW - 72;
	const connInfraNodeStartY = 190;
	const connNodeHalfWidth = 54;
	const cacheNodeHalfWidth = 50;
	const infraNodeHalfWidth = 54;
	const minConnectionSpacing = 28;

	function trimLabel(label: string, max = 14): string {
		if (label.length <= max) return label;
		return `${label.slice(0, max - 1)}…`;
	}

	function infraNodeId(probe: InfraProbe): string {
		return `${probe.kind}-${probe.host}-${probe.port}`;
	}

	const connGateways = $derived(
		gateways.slice(0, 3).map(
			(gw, i): ConnectionNode => ({
				id: gw.apiId,
				x: connGatewayX,
				y: 142 + i * 58,
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
				y: 176 + i * 56,
				label: trimLabel(q.name, 13),
				sub: `${q.approxVisible + q.approxInFlight + q.approxDelayed} msg`,
				kind: 'queue'
			})
		)
	);

	const connBuckets = $derived(
		buckets.slice(0, 4).map(
			(bucket, i): ConnectionNode => ({
				id: bucket.name,
				x: connBucketX,
				y: 318 + i * 44,
				label: trimLabel(bucket.name, 13),
				sub: `${bucket.objects} obj`,
				kind: 'bucket'
			})
		)
	);

	const connFunctions = $derived(
		functions.slice(0, 4).map(
			(fn, i): ConnectionNode => ({
				id: fn.name,
				x: connFunctionX,
				y: 176 + i * 56,
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
				y: 186 + i * 62,
				label: trimLabel(s.name, 13),
				sub: `v${s.versionId.slice(0, 6)}`,
				kind: 'secret'
			})
		)
	);

	const gatewayIdByName = $derived(new Map(gateways.map((gateway) => [gateway.name, gateway.apiId])));
	const queueByName = $derived(new Map(queues.map((queue) => [queue.name, queue])));
	const functionByName = $derived(new Map(functions.map((fn) => [fn.name, fn])));

	const connInfraNodes = $derived(
		(() => {
			const visibleInfra = infra.slice(0, 4).map((probe) => ({ id: infraNodeId(probe), probe }));
			if (visibleInfra.length === 0) return [] as ConnectionNode[];

			const byId = new Map(visibleInfra.map((entry) => [entry.id, entry.probe]));
			const orderedIds = [
				...infraOrderIds.filter((id) => byId.has(id)),
				...visibleInfra.map((entry) => entry.id).filter((id) => !infraOrderIds.includes(id))
			];

			return orderedIds.map((id, i): ConnectionNode => {
				const probe = byId.get(id)!;
				return {
					id,
					x: connInfraX,
					y: connInfraNodeStartY + i * 58,
					label: trimLabel(probe.name, 13),
					sub: '',
					kind: 'infra',
					status: probe.status
				};
			});
		})()
	);

	const connInfraLane = $derived(
		(() => {
			if (connInfraNodes.length === 0) {
				return {
					x: connInfraX - infraNodeHalfWidth - 18,
					y: 146,
					width: infraNodeHalfWidth * 2 + 36,
					height: 144
				};
			}

			const top = Math.min(...connInfraNodes.map((node) => node.y - 16));
			const bottom = Math.max(...connInfraNodes.map((node) => node.y + 16));
			const lanePaddingX = 18;
			const lanePaddingTop = 32;
			const lanePaddingBottom = 20;

			return {
				x: connInfraX - infraNodeHalfWidth - lanePaddingX,
				y: top - lanePaddingTop,
				width: infraNodeHalfWidth * 2 + lanePaddingX * 2,
				height: bottom - top + lanePaddingTop + lanePaddingBottom
			};
		})()
	);

	const connInfraRouteX = $derived(connInfraLane.x - 24);
	const connInfraRouteY = $derived(connInfraLane.y + connInfraLane.height + 14);

	const apigwToQueueEdges = $derived(
		(() => {
			const mapped = infraConnections.flatMap((connection) => {
				if (connection.targetKind !== 'apigw-sqs') return [] as Array<{ from: ConnectionNode; to: ConnectionNode; active: boolean }>;
				const gatewayID = gatewayIdByName.get(connection.sourceFunction) ?? connection.sourceFunction;
				const from = connGateways.find((node) => node.id === gatewayID);
				const queueName = connection.targetId || connection.targetName;
				const to = connQueues.find((node) => node.id === queueName);
				if (!from || !to) return [];

				const queue = queueByName.get(queueName);
				const totalMessages = (queue?.approxVisible ?? 0) + (queue?.approxInFlight ?? 0) + (queue?.approxDelayed ?? 0);

				return [{ from, to, active: totalMessages > 0 }];
			});

			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((edge, lane, arr) => ({
				...edge,
				lane,
				laneCount: arr.length
			}));
		})()
	);

	const apigwToFnEdges = $derived(
		(() => {
			const mapped = infraConnections.flatMap((connection) => {
				if (connection.targetKind !== 'apigw-lambda') return [] as Array<{ from: ConnectionNode; to: ConnectionNode; active: boolean }>;
				const gatewayID = gatewayIdByName.get(connection.sourceFunction) ?? connection.sourceFunction;
				const from = connGateways.find((node) => node.id === gatewayID);
				const functionName = connection.targetId || connection.targetName;
				const to = connFunctions.find((node) => node.id === functionName);
				if (!from || !to) return [];

				const fn = functionByName.get(functionName);
				return [{ from, to, active: (fn?.messagesProcessed ?? 0) > 0 }];
			});

			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((edge, lane, arr) => ({
				...edge,
				lane,
				laneCount: arr.length
			}));
		})()
	);

	const queueToFnEdges = $derived(
		(() => {
			const mapped = eventSourceMappings.flatMap((mapping) => {
				const from = connQueues.find((node) => node.id === mapping.queueName);
				const to = connFunctions.find((node) => node.id === mapping.functionName);
				if (!from || !to) return [] as Array<{ from: ConnectionNode; to: ConnectionNode }>;
				return [{ from, to }];
			});

			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((edge, lane, arr) => ({
				...edge,
				lane,
				laneCount: arr.length
			}));
		})()
	);

	const s3ToFnEdges = $derived(
		(() => {
			const mapped = infraConnections.flatMap((connection) => {
				if (connection.targetKind !== 's3-lambda') return [] as Array<{ from: ConnectionNode; to: ConnectionNode }>;
				const from = connBuckets.find((node) => node.id === connection.sourceFunction);
				const functionName = connection.targetId || connection.targetName;
				const to = connFunctions.find((node) => node.id === functionName);
				if (!from || !to) return [] as Array<{ from: ConnectionNode; to: ConnectionNode }>;
				return [{ from, to }];
			});

			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((edge, lane, arr) => ({
				...edge,
				lane,
				laneCount: arr.length
			}));
		})()
	);

	const fnToInfraEdges = $derived(
		(() => {
			if (connInfraNodes.length === 0 || connFunctions.length === 0 || infraConnections.length === 0) return [];

			const mapped = infraConnections.flatMap(
				(connection): Array<{ from: ConnectionNode; to: ConnectionNode; evidence: InfraConnection['evidence'] }> => {
					const from = connFunctions.find((node) => node.id === connection.sourceFunction);
					const to = connInfraNodes.find((node) => node.id === connection.targetId);
					if (!from || !to) return [];
					return [{ from, to, evidence: connection.evidence }];
				}
			);

			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((edge, lane, arr) => ({
				...edge,
				lane,
				laneCount: arr.length
			}));
		})()
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

	const fnToCacheEdges = $derived(
		[...connFunctions]
			.sort((a, b) => a.y - b.y)
			.map((fn, lane, arr) => ({
				from: fn,
				to: connCacheExtension,
				lane,
				laneCount: arr.length
			}))
	);

	const cacheToSecretEdges = $derived(
		[...connSecrets]
			.sort((a, b) => a.y - b.y)
			.map((secret, lane, arr) => ({
				from: connCacheExtension,
				to: secret,
				lane,
				laneCount: arr.length
			}))
	);

	const connectionCount = $derived(
		apigwToQueueEdges.length +
		apigwToFnEdges.length +
		queueToFnEdges.length +
		s3ToFnEdges.length +
		fnToCacheEdges.length +
		cacheToSecretEdges.length +
		fnToInfraEdges.length
	);
	const selectedGateway = $derived(gateways.find((gateway) => gateway.apiId === selectedGatewayId) ?? null);

	$effect(() => {
		if (selectedGatewayId && !gateways.some((gateway) => gateway.apiId === selectedGatewayId)) {
			selectedGatewayId = '';
		}
	});

	$effect(() => {
		const visibleIds = infra.slice(0, 4).map((probe) => infraNodeId(probe));

		if (typeof window !== 'undefined' && !infraOrderHydrated) {
			infraOrderHydrated = true;
			try {
				const raw = localStorage.getItem(INFRA_ORDER_STORAGE_KEY);
				if (raw) {
					const parsed = JSON.parse(raw);
					if (Array.isArray(parsed)) {
						infraOrderIds = parsed.filter((value): value is string => typeof value === 'string');
					}
				}
			} catch {
				infraOrderIds = [];
			}
		}

		const normalized = [
			...infraOrderIds.filter((id) => visibleIds.includes(id)),
			...visibleIds.filter((id) => !infraOrderIds.includes(id))
		];

		if (normalized.length !== infraOrderIds.length || normalized.some((id, index) => id !== infraOrderIds[index])) {
			infraOrderIds = normalized;
		}
	});

	$effect(() => {
		if (typeof window === 'undefined' || !infraOrderHydrated) return;
		localStorage.setItem(INFRA_ORDER_STORAGE_KEY, JSON.stringify(infraOrderIds));
	});

	function laneAwarePath(
		from: ConnectionNode,
		to: ConnectionNode,
		lane: number,
		laneCount: number,
		fromHalfWidth = connNodeHalfWidth,
		toHalfWidth = connNodeHalfWidth
	): string {
		const movingRight = to.x >= from.x;
		const startX = from.x + (movingRight ? fromHalfWidth : -fromHalfWidth);
		const endX = to.x + (movingRight ? -toHalfWidth : toHalfWidth);
		const deltaX = endX - startX;
		if (Math.abs(deltaX) < 2) {
			return `M ${startX} ${from.y} L ${endX} ${to.y}`;
		}
		const direction = deltaX >= 0 ? 1 : -1;
		const span = Math.abs(deltaX);
		const laneOffset = laneCount > 1 ? (lane - (laneCount - 1) / 2) * 13 : 0;
		const midX = startX + deltaX / 2;
		const midY = (from.y + to.y) / 2 + laneOffset;
		const c1x = startX + direction * span * 0.24;
		const c2x = midX - direction * span * 0.18;
		const c3x = midX + direction * span * 0.18;
		const c4x = endX - direction * span * 0.24;
		return `M ${startX} ${from.y} C ${c1x} ${from.y}, ${c2x} ${midY}, ${midX} ${midY} C ${c3x} ${midY}, ${c4x} ${to.y}, ${endX} ${to.y}`;
	}

	function infraLadderPath(from: ConnectionNode, to: ConnectionNode, lane: number, laneCount: number): string {
		const startX = from.x + connNodeHalfWidth;
		const endX = to.x - infraNodeHalfWidth;
		const laneOffset = laneCount > 1 ? (lane - (laneCount - 1) / 2) * 11 : 0;
		const routeY = Math.max(connInfraRouteY + laneOffset, from.y + 52);
		const routeApproachX = connInfraRouteX - 34;
		const firstC1X = startX + 26;
		const firstC2X = routeApproachX - 34;
		const secondC1X = connInfraRouteX + 24;
		const secondC2X = endX - 26;
		return `M ${startX} ${from.y} C ${firstC1X} ${from.y}, ${firstC2X} ${routeY}, ${routeApproachX} ${routeY} L ${connInfraRouteX} ${routeY} C ${secondC1X} ${routeY}, ${secondC2X} ${to.y}, ${endX} ${to.y}`;
	}

	function moveInfraNode(id: string, direction: -1 | 1) {
		const index = infraOrderIds.indexOf(id);
		if (index === -1) return;
		const nextIndex = index + direction;
		if (nextIndex < 0 || nextIndex >= infraOrderIds.length) return;
		const nextOrder = [...infraOrderIds];
		[nextOrder[index], nextOrder[nextIndex]] = [nextOrder[nextIndex], nextOrder[index]];
		infraOrderIds = nextOrder;
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

		<div class="flex items-center gap-2">
			<span class="text-[10px] text-text-faint font-mono">
				{viewMode === 'components'
					? `${gateways.length + functions.length + queues.length + buckets.length + secrets.length + infra.length} resources`
					: `${connectionCount} links`}
			</span>
			<button
				type="button"
				class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-text-muted transition-colors hover:bg-bg-overlay hover:text-text"
				aria-label={canvasExpanded ? 'Collapse canvas' : 'Expand canvas'}
				title={canvasExpanded ? 'Collapse canvas' : 'Expand canvas'}
				onclick={() => (canvasExpanded = !canvasExpanded)}
			>
				{#if canvasExpanded}
					<ArrowsInSimpleIcon size={13} />
				{:else}
					<ArrowsOutSimpleIcon size={13} />
				{/if}
			</button>
		</div>
	</div>

	{#if viewMode === 'connections' && connInfraNodes.length > 1}
		<div class="border-b border-border px-3 py-2">
			<div class="flex flex-wrap items-center gap-2">
				<span class="text-[10px] font-mono uppercase tracking-wide text-text-faint">Infra Order</span>
				{#each connInfraNodes as node, index (node.id)}
					<div class="inline-flex items-center gap-1 rounded-md border border-border bg-bg-surface px-1.5 py-1">
						<span class="text-[10px] text-text-muted">{node.label}</span>
						<div class="inline-flex gap-0.5">
							<button
								type="button"
								class="inline-flex h-5 w-5 items-center justify-center rounded border border-border text-text-faint disabled:opacity-40"
								disabled={index === 0}
								onclick={() => moveInfraNode(node.id, -1)}
								aria-label={`Move ${node.label} up`}
							>
								<CaretUpIcon size={10} />
							</button>
							<button
								type="button"
								class="inline-flex h-5 w-5 items-center justify-center rounded border border-border text-text-faint disabled:opacity-40"
								disabled={index === connInfraNodes.length - 1}
								onclick={() => moveInfraNode(node.id, 1)}
								aria-label={`Move ${node.label} down`}
							>
								<CaretDownIcon size={10} />
							</button>
						</div>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	<div class="flex flex-col lg:flex-row">
		<div class="relative min-w-0 flex-1 overflow-x-auto">
			{#if viewMode === 'components'}
				<svg
					viewBox="0 0 {W} {H}"
					class="w-full"
					style={`min-width: ${canvasExpanded ? 820 : 480}px; max-height: ${canvasExpanded ? 620 : 380}px;`}
				>
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
					<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.s3} y2={laneY - 16}
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
					{#each s3Positions as pos}
						<line x1={serviceX.s3} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
							stroke="var(--color-accent)" stroke-width="0.75" opacity="0.3" />
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
						<rect x={serviceX.s3 - 36} y={laneY - 14} width="72" height="28" rx="5"
							class="fill-bg-surface" stroke="var(--color-accent)" stroke-width="1" opacity="0.8" />
						<text x={serviceX.s3 - 8} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">
							S3
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

					{#each s3Positions as pos, i}
						{@const b = buckets[i]}
						<g>
							<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
								class="fill-bg-overlay" stroke="var(--color-accent)" stroke-width="0.75" opacity="0.6" />
							<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-accent)" opacity="0.7" />
							<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">
								{b.name.length > 4 ? b.name.slice(0, 4) : b.name}
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
				<svg
					viewBox="0 0 {CW} {CH}"
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

					{#each apigwToQueueEdges as edge}
						<path
							d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount)}
							stroke="var(--color-red)"
							stroke-width={edge.active ? '1.45' : '1.2'}
							fill="none"
							opacity={edge.active ? '0.74' : '0.5'}
							stroke-dasharray={edge.active ? undefined : '5 3'}
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					{/each}

					{#each apigwToFnEdges as edge}
						<path
							d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount)}
							stroke="var(--color-red)"
							stroke-width={edge.active ? '1.45' : '1.2'}
							fill="none"
							opacity={edge.active ? '0.82' : '0.58'}
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					{/each}

					{#each queueToFnEdges as edge}
						<path
							d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount)}
							stroke="var(--color-amber)"
							stroke-width="1.35"
							fill="none"
							opacity="0.72"
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					{/each}

					{#each s3ToFnEdges as edge}
						<path
							d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount)}
							stroke="var(--color-purple, var(--color-accent))"
							stroke-width="1.35"
							fill="none"
							opacity="0.68"
							stroke-dasharray="4 3"
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					{/each}

					{#each fnToCacheEdges as edge}
						<path
							d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount, connNodeHalfWidth, cacheNodeHalfWidth)}
							stroke="var(--color-blue)"
							stroke-width="1.1"
							fill="none"
							opacity="0.52"
							stroke-dasharray="5 4"
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					{/each}

					{#each cacheToSecretEdges as edge}
						<path
							d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount, cacheNodeHalfWidth, connNodeHalfWidth)}
							stroke="var(--color-blue)"
							stroke-width="1.2"
							fill="none"
							opacity="0.62"
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
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

					{#each connBuckets as node}
						<g>
							<rect x={node.x - connNodeHalfWidth} y={node.y - 16} width={connNodeHalfWidth * 2} height="32" rx="6"
								class="fill-bg-overlay" stroke="var(--color-accent)" stroke-width="1" opacity="0.8" />
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
						</g>

						{#each fnToInfraEdges as edge}
							<path
								d={infraLadderPath(edge.from, edge.to, edge.lane, edge.laneCount)}
								stroke="var(--color-accent)"
								stroke-width="1.1"
								fill="none"
								opacity="0.45"
								stroke-dasharray="5 4"
								stroke-linecap="round"
								stroke-linejoin="round"
							/>
						{/each}

						{#each connInfraNodes as node}
							<g>
								<rect x={node.x - infraNodeHalfWidth} y={node.y - 16} width={infraNodeHalfWidth * 2} height="32" rx="6"
									class="fill-bg-overlay" stroke={node.status === 'connected' ? 'var(--color-accent)' : 'var(--color-red)'} stroke-width="1" opacity="0.82" />
								<circle cx={node.x - infraNodeHalfWidth + 12} cy={node.y} r="3"
									fill={node.status === 'connected' ? 'var(--color-accent)' : 'var(--color-red)'}
									opacity="0.8">
									{#if node.status === 'connected'}
										<animate attributeName="opacity" values="0.8;0.4;0.8" dur="3s" repeatCount="indefinite" />
									{/if}
								</circle>
								<text x={node.x + 4} y={node.y + 3} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">
									{node.label}
								</text>
							</g>
						{/each}
					{/if}

						<text x={CW / 2} y={CH - 16} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">
							Red: API Gateway routes · Amber: SQS→Lambda triggers · Purple dashed: S3→Lambda triggers · Green dashed: Lambda→Infra · Blue dashed: Lambda→Secrets cache · Blue solid: cache→secrets
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
