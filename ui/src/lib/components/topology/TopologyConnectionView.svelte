<script lang="ts">
	import type {
		GatewaySummary,
		FunctionSummary,
		QueueSummary,
		BucketSummary,
		SecretSummary,
		InfraProbe,
		EventSourceMappingSummary,
		InfraConnection,
		RequestTrace,
		FilterCriteria
	} from '$lib/types';
	import type { ConnectionNode } from './types';

	let {
		gateways = [],
		functions = [],
		queues = [],
		buckets = [],
		secrets = [],
		infra = [],
		eventSourceMappings = [],
		infraConnections = [],
		infraOrderIds = [],
		recentTraces = [],
		canvasExpanded = false,
		onGatewayClick = (_id: string) => {}
	}: {
		gateways?: GatewaySummary[];
		functions?: FunctionSummary[];
		queues?: QueueSummary[];
		buckets?: BucketSummary[];
		secrets?: SecretSummary[];
		infra?: InfraProbe[];
		eventSourceMappings?: EventSourceMappingSummary[];
		infraConnections?: InfraConnection[];
		infraOrderIds?: string[];
		recentTraces?: RequestTrace[];
		canvasExpanded?: boolean;
		onGatewayClick?: (id: string) => void;
	} = $props();

	// --- Canvas geometry ---
	const CW = 1200;
	const CH = 520;
	const nodeHW = 54; // node half-width
	const cacheHW = 50; // cache extension half-width
	const infraHW = 54; // infra node half-width
	const minConnSpacing = 28;

	// Column X positions (left → right data-flow order)
	const colGW = 88;     // API Gateway
	const colSQS = 252;   // SQS Queues
	const colFn = 484;    // Lambda Functions
	const colSecret = 832; // Secrets Manager — wider gap to breathe with cache ext
	const colS3 = 916;    // S3 Buckets (trigger Lambda, drawn right-to-left)
	const colInfra = CW - 72; // Infrastructure = 1128

	// --- Utility functions ---
	function trimLabel(label: string, max = 14): string {
		return label.length <= max ? label : `${label.slice(0, max - 1)}…`;
	}

	function infraNodeId(probe: InfraProbe): string {
		return `${probe.kind}-${probe.host}-${probe.port}`;
	}

	function laneAwarePath(
		from: ConnectionNode,
		to: ConnectionNode,
		lane: number,
		laneCount: number,
		fromHW = nodeHW,
		toHW = nodeHW
	): string {
		const movingRight = to.x >= from.x;
		const startX = from.x + (movingRight ? fromHW : -fromHW);
		const endX = to.x + (movingRight ? -toHW : toHW);
		const deltaX = endX - startX;
		if (Math.abs(deltaX) < 2) return `M ${startX} ${from.y} L ${endX} ${to.y}`;
		const dir = deltaX >= 0 ? 1 : -1;
		const span = Math.abs(deltaX);
		const laneOffset = laneCount > 1 ? (lane - (laneCount - 1) / 2) * 13 : 0;
		const midX = startX + deltaX / 2;
		const midY = (from.y + to.y) / 2 + laneOffset;
		const c1x = startX + dir * span * 0.24;
		const c2x = midX - dir * span * 0.18;
		const c3x = midX + dir * span * 0.18;
		const c4x = endX - dir * span * 0.24;
		return `M ${startX} ${from.y} C ${c1x} ${from.y}, ${c2x} ${midY}, ${midX} ${midY} C ${c3x} ${midY}, ${c4x} ${to.y}, ${endX} ${to.y}`;
	}

	function infraLadderPath(from: ConnectionNode, to: ConnectionNode, lane: number, laneCount: number): string {
		const startX = from.x + nodeHW;
		const endX = to.x - infraHW;
		const laneOffset = laneCount > 1 ? (lane - (laneCount - 1) / 2) * 11 : 0;
		const routeY = Math.max(connInfraRouteY + laneOffset, from.y + 52);
		const approachX = connInfraRouteX - 34;
		return `M ${startX} ${from.y} C ${startX + 26} ${from.y}, ${approachX - 34} ${routeY}, ${approachX} ${routeY} L ${connInfraRouteX} ${routeY} C ${connInfraRouteX + 24} ${routeY}, ${endX - 26} ${to.y}, ${endX} ${to.y}`;
	}

	// --- Nodes ---
	const connGateways = $derived(
		gateways.slice(0, 3).map((gw, i): ConnectionNode => ({
			id: gw.apiId,
			x: colGW,
			y: 142 + i * 58,
			label: trimLabel(gw.name, 13),
			sub: `${gw.routes} routes`,
			kind: 'gateway'
		}))
	);

	const connQueues = $derived(
		queues.slice(0, 4).map((q, i): ConnectionNode => ({
			id: q.name,
			x: colSQS,
			y: 176 + i * 56,
			label: trimLabel(q.name, 13),
			sub: `${q.approxVisible + q.approxInFlight + q.approxDelayed} msg`,
			kind: 'queue'
		}))
	);

	const connFunctions = $derived(
		functions.slice(0, 4).map((fn, i): ConnectionNode => ({
			id: fn.name,
			x: colFn,
			y: 176 + i * 56,
			label: trimLabel(fn.name, 13),
			sub: fn.runtime,
			kind: 'function'
		}))
	);

	const connBuckets = $derived(
		buckets.slice(0, 4).map((b, i): ConnectionNode => ({
			id: b.name,
			x: colS3,
			y: 318 + i * 44,
			label: trimLabel(b.name, 13),
			sub: `${b.objects} obj`,
			kind: 'bucket'
		}))
	);

	const connSecrets = $derived(
		secrets.slice(0, 3).map((s, i): ConnectionNode => ({
			id: s.name,
			x: colSecret,
			y: 186 + i * 62,
			label: trimLabel(s.name, 13),
			sub: `v${s.versionId.slice(0, 6)}`,
			kind: 'secret'
		}))
	);

	// Infrastructure nodes (respects ordering from parent)
	const connInfraNodes = $derived(
		(() => {
			const visible = infra.slice(0, 4).map((probe) => ({ id: infraNodeId(probe), probe }));
			if (visible.length === 0) return [] as ConnectionNode[];
			const byId = new Map(visible.map((e) => [e.id, e.probe]));
			const orderedIds = [
				...infraOrderIds.filter((id) => byId.has(id)),
				...visible.map((e) => e.id).filter((id) => !infraOrderIds.includes(id))
			];
			return orderedIds.map((id, i): ConnectionNode => {
				const probe = byId.get(id)!;
				return { id, x: colInfra, y: 190 + i * 58, label: trimLabel(probe.name, 13), sub: '', kind: 'infra', status: probe.status };
			});
		})()
	);

	// Infrastructure lane bounding box
	const connInfraLane = $derived(
		(() => {
			const padX = 18, padTop = 32, padBottom = 20;
			if (connInfraNodes.length === 0) {
				return { x: colInfra - infraHW - padX, y: 146, width: infraHW * 2 + padX * 2, height: 144 };
			}
			const top = Math.min(...connInfraNodes.map((n) => n.y - 16));
			const bottom = Math.max(...connInfraNodes.map((n) => n.y + 16));
			return {
				x: colInfra - infraHW - padX,
				y: top - padTop,
				width: infraHW * 2 + padX * 2,
				height: bottom - top + padTop + padBottom
			};
		})()
	);

	const connInfraRouteX = $derived(connInfraLane.x - 24);
	const connInfraRouteY = $derived(connInfraLane.y + connInfraLane.height + 14);

	// Secrets cache extension — centred symmetrically between Lambda and Secrets columns
	const cacheTargetX = Math.round((colFn + nodeHW + (colSecret - nodeHW)) / 2);
	const cacheMinX = colFn + nodeHW + cacheHW + 20;
	const cacheMaxX = colSecret - nodeHW - cacheHW - 20;

	const connCacheExtension = $derived<ConnectionNode>({
		id: 'secrets-cache-extension',
		x: cacheMinX <= cacheMaxX ? Math.max(cacheMinX, Math.min(cacheMaxX, cacheTargetX)) : cacheTargetX,
		y: CH / 2 + 10,
		label: 'Secrets Cache',
		sub: 'localhost:2773',
		kind: 'extension'
	});

	// --- Lookup maps ---
	const gatewayIdByName = $derived(new Map(gateways.map((gw) => [gw.name, gw.apiId])));
	const queueByName = $derived(new Map(queues.map((q) => [q.name, q])));
	const functionByName = $derived(new Map(functions.map((fn) => [fn.name, fn])));

	// --- Edges ---
	const apigwToQueueEdges = $derived(
		(() => {
			const mapped = infraConnections.flatMap((c) => {
				if (c.targetKind !== 'apigw-sqs') return [];
				const gwId = gatewayIdByName.get(c.sourceFunction) ?? c.sourceFunction;
				const from = connGateways.find((n) => n.id === gwId);
				const qName = c.targetId || c.targetName;
				const to = connQueues.find((n) => n.id === qName);
				if (!from || !to) return [];
				const q = queueByName.get(qName);
				const total = (q?.approxVisible ?? 0) + (q?.approxInFlight ?? 0) + (q?.approxDelayed ?? 0);
				return [{ from, to, active: total > 0 }];
			});
			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((e, lane, arr) => ({ ...e, lane, laneCount: arr.length }));
		})()
	);

	const apigwToFnEdges = $derived(
		(() => {
			const mapped = infraConnections.flatMap((c) => {
				if (c.targetKind !== 'apigw-lambda') return [];
				const gwId = gatewayIdByName.get(c.sourceFunction) ?? c.sourceFunction;
				const from = connGateways.find((n) => n.id === gwId);
				const fnName = c.targetId || c.targetName;
				const to = connFunctions.find((n) => n.id === fnName);
				if (!from || !to) return [];
				const fn = functionByName.get(fnName);
				return [{ from, to, active: (fn?.messagesProcessed ?? 0) > 0 }];
			});
			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((e, lane, arr) => ({ ...e, lane, laneCount: arr.length }));
		})()
	);

	const queueToFnEdges = $derived(
		(() => {
			// Union both eventSourceMappings and infraConnections sqs-lambda entries, deduplicated.
			// Prefer ESM data (has filter criteria) over plain infraConnection entries.
			const pairs: { queueId: string; fnId: string; filterCriteria?: FilterCriteria }[] = [];
			const hasKey = (q: string, f: string) => pairs.some((p) => p.queueId === q && p.fnId === f);

			for (const m of eventSourceMappings) {
				if (!hasKey(m.queueName, m.functionName))
					pairs.push({ queueId: m.queueName, fnId: m.functionName, filterCriteria: m.filterCriteria });
			}
			for (const c of infraConnections) {
				if (c.targetKind !== 'sqs-lambda') continue;
				const fnId = c.targetId || c.targetName;
				if (!hasKey(c.sourceFunction, fnId))
					pairs.push({ queueId: c.sourceFunction, fnId, filterCriteria: c.filterCriteria });
			}

			const mapped = pairs.flatMap(({ queueId, fnId, filterCriteria }) => {
				const from = connQueues.find((n) => n.id === queueId);
				const to = connFunctions.find((n) => n.id === fnId);
				if (!from || !to) return [];
				return [{ from, to, filterCriteria }];
			});
			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((e, lane, arr) => ({ ...e, lane, laneCount: arr.length }));
		})()
	);

	// Extracts a short human-readable label from a filter criteria pattern.
	// e.g. {"body":{"type":["order"]}} → 'type=order'
	function filterLabel(fc: FilterCriteria | undefined): string | null {
		if (!fc || fc.Filters.length === 0) return null;
		try {
			const pattern = JSON.parse(fc.Filters[0].Pattern);
			const bodyConditions = pattern.body;
			if (bodyConditions && typeof bodyConditions === 'object') {
				const parts = Object.entries(bodyConditions).map(([k, v]) => {
					const arr = Array.isArray(v) ? v : [v];
					return `${k}=${arr.slice(0, 2).join('|')}`;
				});
				return parts.slice(0, 2).join(',');
			}
		} catch {
			// unparseable — show first 16 chars of raw pattern
			const raw = fc.Filters[0].Pattern;
			return raw.length > 16 ? raw.slice(0, 15) + '…' : raw;
		}
		return null;
	}

	const s3ToFnEdges = $derived(
		(() => {
			const mapped = infraConnections.flatMap((c) => {
				if (c.targetKind !== 's3-lambda') return [];
				const from = connBuckets.find((n) => n.id === c.sourceFunction);
				const fnName = c.targetId || c.targetName;
				const to = connFunctions.find((n) => n.id === fnName);
				if (!from || !to) return [];
				return [{ from, to }];
			});
			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((e, lane, arr) => ({ ...e, lane, laneCount: arr.length }));
		})()
	);

	// Queue → DLQ edges (same-column arc, bulges left of SQS column)
	const queueToDlqEdges = $derived(
		infraConnections.flatMap((c) => {
			if (c.targetKind !== 'queue-dlq') return [];
			const from = connQueues.find((n) => n.id === c.sourceFunction);
			const to = connQueues.find((n) => n.id === (c.targetId || c.targetName));
			if (!from || !to || from.id === to.id) return [];
			return [{ from, to }];
		})
	);

	// Left-bulging arc for two nodes in the same column
	function dlqArcPath(from: ConnectionNode, to: ConnectionNode): string {
		const startX = from.x - nodeHW;
		const endX = to.x - nodeHW;
		const bulge = 50;
		return `M ${startX} ${from.y} C ${startX - bulge} ${from.y}, ${endX - bulge} ${to.y}, ${endX} ${to.y}`;
	}

	// AWS service connection kinds handled by other edge derivations (excluded from infra ladder)
	const awsServiceKinds = ['apigw-sqs', 'apigw-lambda', 's3-lambda', 'sqs-lambda', 'queue-dlq'];

	const fnToInfraEdges = $derived(
		(() => {
			if (!connInfraNodes.length || !connFunctions.length || !infraConnections.length) return [];
			const mapped = infraConnections.flatMap((c) => {
				if (awsServiceKinds.includes(c.targetKind)) return []; // skip AWS-managed edges
				const from = connFunctions.find((n) => n.id === c.sourceFunction);
				const to = connInfraNodes.find((n) => n.id === c.targetId);
				if (!from || !to) return [];
				return [{ from, to }];
			});
			const sorted = mapped.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
			return sorted.map((e, lane, arr) => ({ ...e, lane, laneCount: arr.length }));
		})()
	);

	const fnToCacheEdges = $derived(
		[...connFunctions]
			.sort((a, b) => a.y - b.y)
			.map((fn, lane, arr) => ({ from: fn, to: connCacheExtension, lane, laneCount: arr.length }))
	);

	const cacheToSecretEdges = $derived(
		[...connSecrets]
			.sort((a, b) => a.y - b.y)
			.map((secret, lane, arr) => ({ from: connCacheExtension, to: secret, lane, laneCount: arr.length }))
	);

	const hasData = $derived(
		gateways.length > 0 || functions.length > 0 || queues.length > 0 ||
		buckets.length > 0 || secrets.length > 0 || infra.length > 0
	);

	// --- Trace activity ---
	// Tracks which edges had traffic in the last 60s.
	// Key namespaces: gw:: GW→Lambda/SQS, queue:: ESM SQS→Lambda, dlq:: queue→DLQ moves
	const TRACE_WINDOW_MS = 60_000;
	let selectedTraceId = $state<string | null>(null);

	interface EdgeActivity { count: number; hasError: boolean; latestMs: number }

	const traceEdgeActivity = $derived(
		(() => {
			const now = Date.now();
			const map = new Map<string, EdgeActivity>();
			for (const t of recentTraces) {
				const age = now - new Date(t.startedAt).getTime();
				if (age > TRACE_WINDOW_MS) continue;
				const lambdaSpan = t.spans.find((s) => s.kind === 'lambda');
				const queueSpan = t.spans.find((s) => s.kind === 'queue');
				const dlqSpan = t.spans.find((s) => s.kind === 'dlq');

				let key: string | null = null;
				if (t.gatewayId) {
					// API Gateway triggered: GW→Lambda or GW→SQS
					const target = lambdaSpan ? lambdaSpan.name : queueSpan ? queueSpan.name : null;
					if (target) key = `gw::${t.gatewayId}→${target}`;
				} else if (queueSpan && dlqSpan) {
					// DLQ move: SQS→DLQ (no lambda, no gateway)
					key = `dlq::${queueSpan.name}→${dlqSpan.name}`;
				} else if (queueSpan && lambdaSpan) {
					// ESM triggered: SQS→Lambda (no gateway)
					key = `queue::${queueSpan.name}→${lambdaSpan.name}`;
				}
				if (!key) continue;

				const existing = map.get(key) ?? { count: 0, hasError: false, latestMs: 0 };
				existing.count++;
				if (t.status >= 500) existing.hasError = true;
				if (t.durationMs > existing.latestMs) existing.latestMs = t.durationMs;
				map.set(key, existing);
			}
			return map;
		})()
	);

	// Edge activity for a GW→Lambda pair
	function gwFnActivity(gwId: string, fnName: string): EdgeActivity | undefined {
		return traceEdgeActivity.get(`gw::${gwId}→${fnName}`);
	}
	// Edge activity for a GW→Queue pair
	function gwQueueActivity(gwId: string, queueName: string): EdgeActivity | undefined {
		return traceEdgeActivity.get(`gw::${gwId}→${queueName}`);
	}
	// Edge activity for a SQS→Lambda (ESM) pair
	function queueFnActivity(queueName: string, fnName: string): EdgeActivity | undefined {
		return traceEdgeActivity.get(`queue::${queueName}→${fnName}`);
	}
	// Edge activity for a Queue→DLQ move
	function queueDlqActivity(srcQueue: string, dlqName: string): EdgeActivity | undefined {
		return traceEdgeActivity.get(`dlq::${srcQueue}→${dlqName}`);
	}
	// Any recent activity touching a given function (GW or ESM triggered)
	function fnActivity(fnName: string): EdgeActivity | undefined {
		for (const [key, act] of traceEdgeActivity) {
			if (key.endsWith(`→${fnName}`)) return act;
		}
		return undefined;
	}

	// Aggregate activity across all functions (used for cache→secret edges)
	const cacheActivity = $derived(
		(() => {
			let count = 0, hasError = false, latestMs = 0;
			for (const fn of connFunctions) {
				const act = fnActivity(fn.id);
				if (act) { count += act.count; hasError = hasError || act.hasError; latestMs = Math.max(latestMs, act.latestMs); }
			}
			return count > 0 ? { count, hasError, latestMs } : undefined;
		})()
	);

	// Color for infra edges/nodes by probe kind
	function infraKindColor(kind: string): string {
		switch (kind?.toLowerCase()) {
			case 'postgres': case 'postgresql': case 'mysql': return 'var(--color-blue)';
			case 'redis': return 'var(--color-amber)';
			case 'http': return 'var(--color-purple, var(--color-accent))';
			default: return 'var(--color-accent)';
		}
	}

	// The last N traces to show in the ticker
	const tickerTraces = $derived(recentTraces.slice(0, 8));

	const selectedTrace = $derived(tickerTraces.find((t) => t.id === selectedTraceId) ?? null);

	function traceStatusColor(status: number): string {
		if (status >= 500) return 'var(--color-red)';
		if (status >= 400) return 'var(--color-amber)';
		return 'var(--color-accent)';
	}

	function activityStroke(activity: EdgeActivity | undefined, defaultStroke: string): string {
		if (!activity) return defaultStroke;
		return activity.hasError ? 'var(--color-red)' : 'var(--color-accent)';
	}

	function activityOpacity(activity: EdgeActivity | undefined, base: number): number {
		if (!activity) return base;
		return Math.min(0.95, base + activity.count * 0.12);
	}

	function activityWidth(activity: EdgeActivity | undefined, base: number): number {
		if (!activity) return base;
		return Math.min(2.4, base + activity.count * 0.2);
	}

	function handleGatewayKeydown(event: KeyboardEvent, id: string) {
		if (event.key === 'Enter' || event.key === ' ') {
			event.preventDefault();
			onGatewayClick(id);
		}
	}
</script>

<svg
	viewBox="0 0 {CW} {CH}"
	class="w-full"
	style={`min-width: ${canvasExpanded ? 1160 : 720}px; max-height: ${canvasExpanded ? 780 : 480}px;`}
>
	<!-- Dot grid -->
	{#each Array(Math.floor(CW / 48)) as _, ix (ix)}
		{#each Array(Math.floor(CH / 48)) as _, iy (iy)}
			<circle cx={24 + ix * 48} cy={24 + iy * 48} r="0.55" class="fill-border" opacity="0.45" />
		{/each}
	{/each}

	{#if hasData}
		<!-- Infra edges rendered first so they sit behind all other elements -->
		{#if fnToInfraEdges.length > 0}
			{#each fnToInfraEdges as edge (edge.lane)}
				{@const act = fnActivity(edge.from.id)}
				{@const probe = infra.find((p) => infraNodeId(p) === edge.to.id)}
				{@const isConnected = probe?.status === 'connected'}
				{@const isActive = !!act && isConnected}
				{@const edgeColor = infraKindColor(probe?.kind ?? '')}
				<path
					d={infraLadderPath(edge.from, edge.to, edge.lane, edge.laneCount)}
					stroke={isActive && act?.hasError ? 'var(--color-red)' : (!isConnected ? 'var(--color-red)' : edgeColor)}
					stroke-width={activityWidth(isActive ? act : undefined, 1.0)}
					fill="none"
					opacity={activityOpacity(isActive ? act : undefined, isConnected ? 0.28 : 0.16)}
					stroke-dasharray={isActive ? '6 3' : (isConnected ? '5 4' : '3 3')}
					stroke-linecap="round"
					stroke-linejoin="round"
					class={isActive ? 'trace-flow' : undefined}
				/>
				{#if isActive && act}
					<text
						x={connInfraRouteX + (edge.lane - (edge.laneCount - 1) / 2) * 11}
						y={connInfraRouteY - 5}
						text-anchor="middle"
						fill={edgeColor}
						font-size="6.5"
						font-family="var(--font-mono)"
						opacity="0.72"
					>{act.latestMs}ms</text>
				{/if}
			{/each}
		{/if}

		<!-- Column labels -->
		<text x={colGW} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">API Gateway</text>
		<text x={colSQS} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">SQS</text>
		<text x={colFn} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">Lambda</text>
		{#if connSecrets.length > 0}
			<text x={connCacheExtension.x} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">Cache Ext</text>
			<text x={colSecret} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">Secrets</text>
		{/if}
		{#if connBuckets.length > 0}
			<text x={colS3} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">S3</text>
		{/if}
		{#if infra.length > 0}
			<text x={colInfra} y={92} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">Infra</text>
		{/if}

		<!-- Edges: API Gateway → SQS Queue -->
		{#each apigwToQueueEdges as edge (edge.lane)}
			{@const act = gwQueueActivity(edge.from.id, edge.to.id)}
			<path
				d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount)}
				stroke={activityStroke(act, 'var(--color-red)')}
				stroke-width={activityWidth(act, edge.active ? 1.45 : 1.2)}
				fill="none"
				opacity={activityOpacity(act, edge.active ? 0.74 : 0.5)}
				stroke-dasharray={act ? '6 3' : (edge.active ? undefined : '5 3')}
				stroke-linecap="round"
				stroke-linejoin="round"
				class={act ? 'trace-flow' : undefined}
			/>
		{/each}

		<!-- Edges: API Gateway → Lambda -->
		{#each apigwToFnEdges as edge (edge.lane)}
			{@const act = gwFnActivity(edge.from.id, edge.to.id)}
			<path
				d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount)}
				stroke={activityStroke(act, 'var(--color-red)')}
				stroke-width={activityWidth(act, edge.active ? 1.45 : 1.2)}
				fill="none"
				opacity={activityOpacity(act, edge.active ? 0.82 : 0.58)}
				stroke-linecap="round"
				stroke-linejoin="round"
				stroke-dasharray={act ? '6 3' : undefined}
				class={act ? 'trace-flow' : undefined}
			/>
			{#if act}
				<text
					x={(edge.from.x + edge.to.x) / 2}
					y={(edge.from.y + edge.to.y) / 2 - 7}
					text-anchor="middle"
					class="fill-accent"
					font-size="7"
					font-family="var(--font-mono)"
					opacity="0.85"
				>{act.count} req · {act.latestMs}ms</text>
			{/if}
		{/each}

		<!-- Edges: SQS → Lambda -->
		{#each queueToFnEdges as edge (edge.lane)}
			{@const act = queueFnActivity(edge.from.id, edge.to.id)}
			{@const flabel = filterLabel(edge.filterCriteria)}
			<path
				d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount)}
				stroke={activityStroke(act, 'var(--color-amber)')}
				stroke-width={activityWidth(act, 1.35)}
				fill="none"
				opacity={activityOpacity(act, 0.72)}
				stroke-dasharray={act ? '6 3' : undefined}
				stroke-linecap="round"
				stroke-linejoin="round"
				class={act ? 'trace-flow' : undefined}
			/>
			{#if act}
				<text
					x={(edge.from.x + edge.to.x) / 2}
					y={(edge.from.y + edge.to.y) / 2 - 7}
					text-anchor="middle"
					class="fill-amber"
					font-size="7"
					font-family="var(--font-mono)"
					opacity="0.85"
				>{act.count} msg · {act.latestMs}ms</text>
			{:else if flabel}
				<text
					x={(edge.from.x + edge.to.x) / 2}
					y={(edge.from.y + edge.to.y) / 2 - 6}
					text-anchor="middle"
					class="fill-amber"
					font-size="6.5"
					font-family="var(--font-mono)"
					opacity="0.65"
				>⊘ {flabel}</text>
			{/if}
		{/each}

		<!-- Edges: Queue → DLQ (same-column left-bulging arc) -->
		{#each queueToDlqEdges as edge (edge.from.id + '→' + edge.to.id)}
			{@const act = queueDlqActivity(edge.from.id, edge.to.id)}
			<path
				d={dlqArcPath(edge.from, edge.to)}
				stroke={act ? 'var(--color-red)' : 'var(--color-red)'}
				stroke-width={activityWidth(act, 1.2)}
				fill="none"
				opacity={activityOpacity(act, 0.45)}
				stroke-dasharray={act ? '5 2' : '3 3'}
				stroke-linecap="round"
				stroke-linejoin="round"
				class={act ? 'trace-flow' : undefined}
			/>
			{#if act}
				<text
					x={edge.from.x - nodeHW - 26}
					y={(edge.from.y + edge.to.y) / 2}
					text-anchor="middle"
					class="fill-red"
					font-size="7"
					font-family="var(--font-mono)"
					opacity="0.85"
				>{act.count} dlq</text>
			{/if}
		{/each}

		<!-- Edges: S3 → Lambda (right-to-left trigger) -->
		{#each s3ToFnEdges as edge (edge.lane)}
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

		<!-- Edges: Lambda → Secrets Cache → Secrets Manager -->
		{#if connSecrets.length > 0}
			{#each fnToCacheEdges as edge (edge.from.id)}
				{@const act = fnActivity(edge.from.id)}
				<path
					d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount, nodeHW, cacheHW)}
					stroke={act?.hasError ? 'var(--color-red)' : 'var(--color-blue)'}
					stroke-width={activityWidth(act, 1.1)}
					fill="none"
					opacity={activityOpacity(act, 0.48)}
					stroke-dasharray={act ? '6 3' : '5 4'}
					stroke-linecap="round"
					stroke-linejoin="round"
					class={act ? 'trace-flow' : undefined}
				/>
			{/each}
			{#if cacheActivity}
				<text
					x={connCacheExtension.x}
					y={connCacheExtension.y - 26}
					text-anchor="middle"
					class="fill-blue"
					font-size="7"
					font-family="var(--font-mono)"
					opacity="0.82"
				>{cacheActivity.count} call{cacheActivity.count !== 1 ? 's' : ''} · {cacheActivity.latestMs}ms</text>
			{/if}
			{#each cacheToSecretEdges as edge (edge.to.id)}
				<path
					d={laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount, cacheHW, nodeHW)}
					stroke={cacheActivity?.hasError ? 'var(--color-red)' : 'var(--color-blue)'}
					stroke-width={activityWidth(cacheActivity, 1.2)}
					fill="none"
					opacity={activityOpacity(cacheActivity, 0.58)}
					stroke-dasharray={cacheActivity ? '6 3' : undefined}
					stroke-linecap="round"
					stroke-linejoin="round"
					class={cacheActivity ? 'trace-flow' : undefined}
				/>
			{/each}
		{/if}

		<!-- Nodes: API Gateways -->
		{#each connGateways as node (node.id)}
			<g
				role="button"
				tabindex="0"
				aria-label={`Open API Gateway details for ${node.label}`}
				class="cursor-pointer"
				onclick={() => onGatewayClick(node.id)}
				onkeydown={(e: KeyboardEvent) => handleGatewayKeydown(e, node.id)}
			>
				<rect
					x={node.x - nodeHW} y={node.y - 16} width={nodeHW * 2} height="32" rx="6"
					class="fill-bg-overlay" stroke="var(--color-red)" stroke-width="1" opacity="0.84"
				/>
				<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">{node.label}</text>
				<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">{node.sub}</text>
			</g>
		{/each}

		<!-- Nodes: SQS Queues -->
		{#each connQueues as node (node.id)}
			<g>
				<rect
					x={node.x - nodeHW} y={node.y - 16} width={nodeHW * 2} height="32" rx="6"
					class="fill-bg-overlay" stroke="var(--color-amber)" stroke-width="1" opacity="0.8"
				/>
				<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">{node.label}</text>
				<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">{node.sub}</text>
			</g>
		{/each}

		<!-- Nodes: Lambda Functions -->
		{#each connFunctions as node (node.id)}
			<g>
				<rect
					x={node.x - nodeHW} y={node.y - 16} width={nodeHW * 2} height="32" rx="6"
					class="fill-bg-overlay" stroke="var(--color-accent)" stroke-width="1" opacity="0.82"
				/>
				<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">{node.label}</text>
				<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">{node.sub}</text>
			</g>
		{/each}

		<!-- Node: Secrets Cache Extension -->
		{#if connSecrets.length > 0}
			<g>
				<rect
					x={connCacheExtension.x - cacheHW} y={connCacheExtension.y - 18} width={cacheHW * 2} height="36" rx="7"
					class="fill-bg-overlay" stroke="var(--color-blue)" stroke-width="1.1" opacity="0.86"
				/>
				<text x={connCacheExtension.x} y={connCacheExtension.y - 3} text-anchor="middle" class="fill-text" font-size="8.4" font-family="var(--font-mono)">{connCacheExtension.label}</text>
				<text x={connCacheExtension.x} y={connCacheExtension.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">{connCacheExtension.sub}</text>
			</g>
		{/if}

		<!-- Nodes: Secrets Manager -->
		{#each connSecrets as node (node.id)}
			<g>
				<rect
					x={node.x - nodeHW} y={node.y - 16} width={nodeHW * 2} height="32" rx="6"
					class="fill-bg-overlay" stroke="var(--color-blue)" stroke-width="1" opacity="0.82"
				/>
				<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">{node.label}</text>
				<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">{node.sub}</text>
			</g>
		{/each}

		<!-- Nodes: S3 Buckets -->
		{#each connBuckets as node (node.id)}
			<g>
				<rect
					x={node.x - nodeHW} y={node.y - 16} width={nodeHW * 2} height="32" rx="6"
					class="fill-bg-overlay" stroke="var(--color-accent)" stroke-width="1" opacity="0.8"
				/>
				<text x={node.x} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">{node.label}</text>
				<text x={node.x} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="7" font-family="var(--font-mono)">{node.sub}</text>
			</g>
		{/each}

		<!-- Infrastructure Ladder (box + nodes on top of all other elements) -->
		{#if infra.length > 0}
			<rect
				x={connInfraLane.x} y={connInfraLane.y}
				width={connInfraLane.width} height={connInfraLane.height}
				rx="12" class="fill-bg-surface stroke-border" stroke-width="1" opacity="0.42"
			/>
			<text
				x={connInfraLane.x + 18} y={connInfraLane.y + 18}
				class="fill-text-muted" font-size="8.5" font-family="var(--font-mono)"
			>Infrastructure</text>

			{#each connInfraNodes as node (node.id)}
				{@const probe = infra.find((p) => infraNodeId(p) === node.id)}
				{@const kindColor = infraKindColor(probe?.kind ?? '')}
				{@const isConnected = node.status === 'connected'}
				<g>
					<rect
						x={node.x - infraHW} y={node.y - 16} width={infraHW * 2} height="32" rx="6"
						class="fill-bg-overlay"
						stroke={isConnected ? kindColor : 'var(--color-red)'}
						stroke-width="1" opacity="0.82"
					/>
					<circle
						cx={node.x - infraHW + 12} cy={node.y} r="3"
						fill={isConnected ? kindColor : 'var(--color-red)'}
						opacity="0.8"
					>
						{#if isConnected}
							<animate attributeName="opacity" values="0.8;0.35;0.8" dur="2.4s" repeatCount="indefinite" />
						{/if}
					</circle>
					<text x={node.x + 4} y={node.y - 2} text-anchor="middle" class="fill-text" font-size="8.2" font-family="var(--font-mono)">{node.label}</text>
					{#if probe?.version}
						<text x={node.x + 4} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="6.5" font-family="var(--font-mono)">{probe.version}</text>
					{:else if (probe?.latencyMs ?? 0) > 0}
						<text x={node.x + 4} y={node.y + 9} text-anchor="middle" class="fill-text-faint" font-size="6.5" font-family="var(--font-mono)">{probe.latencyMs.toFixed(0)}ms</text>
					{/if}
				</g>
			{/each}
		{/if}

		<!-- Legend -->
		<text x={CW / 2} y={CH - 16} text-anchor="middle" class="fill-text-faint" font-size="8" font-family="var(--font-mono)">
			Red: APIGW · Amber: SQS→Lambda · Red arc: DLQ · Purple dashed: S3 · Blue: Secrets · DB: blue · Redis: amber · HTTP: purple · animated: active
		</text>

		<!-- Trace ticker — last 8 traces shown at bottom right -->
		{#if tickerTraces.length > 0}
			{@const tickerX = CW - 16}
			{@const tickerStartY = CH - 22 - (tickerTraces.length - 1) * 14}
			<text
				x={tickerX} y={tickerStartY - 10}
				text-anchor="end" class="fill-text-faint" font-size="7.5" font-family="var(--font-mono)"
				opacity="0.7"
			>recent requests</text>
			{#each tickerTraces as t, i (t.id)}
				<g
					role="button"
					tabindex="0"
					class="cursor-pointer"
					onclick={() => { selectedTraceId = selectedTraceId === t.id ? null : t.id; }}
					onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') selectedTraceId = selectedTraceId === t.id ? null : t.id; }}
				>
					<rect
						x={tickerX - 160} y={tickerStartY + i * 14 - 9}
						width="164" height="12" rx="2"
						fill={selectedTraceId === t.id ? 'var(--color-accent)' : 'transparent'}
						opacity={selectedTraceId === t.id ? '0.12' : '1'}
					/>
					<circle
						cx={tickerX - 152} cy={tickerStartY + i * 14 - 3}
						r="3"
						fill={traceStatusColor(t.status)}
						opacity="0.85"
					/>
					<text
						x={tickerX - 145} y={tickerStartY + i * 14}
						class="fill-text-muted" font-size="7.5" font-family="var(--font-mono)"
					>{t.method ?? ''} {t.path ?? ''}</text>
					<text
						x={tickerX} y={tickerStartY + i * 14}
						text-anchor="end" class="fill-text-faint" font-size="7" font-family="var(--font-mono)"
					>{t.durationMs}ms · {t.status}</text>
				</g>
			{/each}
		{/if}

		<!-- Selected trace highlight: pulse matched nodes for the selected trace -->
		{#if selectedTrace}
			{@const lambdaSpan = selectedTrace.spans.find((s) => s.kind === 'lambda')}
			{@const queueSpan = selectedTrace.spans.find((s) => s.kind === 'queue')}
			{@const dlqSpan = selectedTrace.spans.find((s) => s.kind === 'dlq')}
			{@const matchedGW = selectedTrace.gatewayId ? connGateways.find((n) => n.id === selectedTrace.gatewayId) : null}
			{@const matchedFn = lambdaSpan ? connFunctions.find((n) => n.id === lambdaSpan.name) : null}
			{@const matchedQ = queueSpan ? connQueues.find((n) => n.id === queueSpan.name) : null}
			{@const matchedDLQ = dlqSpan ? connQueues.find((n) => n.id === dlqSpan.name) : null}
			{#each [matchedGW, matchedFn, matchedQ, matchedDLQ].filter(Boolean) as node}
				<rect
					x={node!.x - nodeHW - 3} y={node!.y - 19}
					width={nodeHW * 2 + 6} height="38" rx="8"
					fill="none"
					stroke={traceStatusColor(selectedTrace.status)}
					stroke-width="1.5"
					opacity="0.7"
					stroke-dasharray="4 2"
				>
					<animate attributeName="opacity" values="0.7;0.3;0.7" dur="1.8s" repeatCount="indefinite" />
				</rect>
			{/each}
		{/if}
	{:else}
		<text x={CW / 2} y={CH / 2} text-anchor="middle" class="fill-text-faint" font-size="11" font-family="var(--font-mono)">
			No architecture data
		</text>
	{/if}
</svg>

<style>
	/* Animated flowing dash for active trace edges */
	:global(.trace-flow) {
		animation: dash-flow 1.4s linear infinite;
	}
	@keyframes dash-flow {
		from { stroke-dashoffset: 18; }
		to   { stroke-dashoffset: 0; }
	}
</style>
