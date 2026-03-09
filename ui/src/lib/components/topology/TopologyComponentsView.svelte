<script lang="ts">
	import { getDashboard } from '$lib/state.svelte';
	import type {
		GatewaySummary,
		FunctionSummary,
		QueueSummary,
		SecretSummary,
		BucketSummary,
		InfraProbe
	} from '$lib/types';

	let {
		gateways = [],
		functions = [],
		queues = [],
		secrets = [],
		buckets = [],
		infra = [],
		canvasExpanded = false,
		onGatewayClick = (_id: string) => {}
	}: {
		gateways?: GatewaySummary[];
		functions?: FunctionSummary[];
		queues?: QueueSummary[];
		secrets?: SecretSummary[];
		buckets?: BucketSummary[];
		infra?: InfraProbe[];
		canvasExpanded?: boolean;
		onGatewayClick?: (id: string) => void;
	} = $props();

	const dashboard = getDashboard();

	// --- Canvas geometry ---
	const W = 960;
	const H = 380;
	const CX = W / 2;
	const endpoint = { x: CX, y: 52 };
	const laneY = 150;

	// AWS service columns — infra is no longer in this lane
	const serviceX = { apigw: 80, lambda: 220, sqs: 370, secrets: 520, s3: 670 };

	// Local env zone — right panel
	const ZONE_X = 748;          // vertical separator position
	const ZONE_CX = ZONE_X + (W - ZONE_X) / 2;   // center of zone
	const ZONE_ROW_START_Y = 66; // center Y of first probe row
	const ZONE_ROW_H = 28;       // row height

	// --- Colors ---
	const ledColorMap: Record<string, string> = {
		green: 'var(--color-accent)',
		amber: 'var(--color-amber)',
		red: 'var(--color-red)',
		gray: 'var(--color-text-faint)'
	};

	function stateColor(state: string): string {
		const s = state?.toLowerCase() ?? '';
		if (s === 'active' || s === 'running') return 'green';
		if (s === 'pending') return 'amber';
		if (s === 'failed' || s === 'inactive') return 'red';
		return 'gray';
	}

	function probeColor(probe: InfraProbe): string {
		if (probe.status !== 'connected') return 'var(--color-red)';
		if (probe.kind === 'http' || probe.kind === 'https') return 'var(--color-blue)';
		return 'var(--color-accent)';
	}

	// Fan out resource positions around their service column
	function fanPositions(cx: number, count: number, baseY: number, spacing: number): { x: number; y: number }[] {
		if (count === 0) return [];
		const totalWidth = (count - 1) * spacing;
		const startX = cx - totalWidth / 2;
		return Array.from({ length: count }, (_, i) => ({ x: startX + i * spacing, y: baseY }));
	}

	const gwPositions = $derived(fanPositions(serviceX.apigw, Math.min(gateways.length, 6), laneY + 90, 40));
	const fnPositions = $derived(fanPositions(serviceX.lambda, Math.min(functions.length, 6), laneY + 90, 48));
	const qPositions  = $derived(fanPositions(serviceX.sqs,    Math.min(queues.length,    6), laneY + 90, 48));
	const sPositions  = $derived(fanPositions(serviceX.secrets, Math.min(secrets.length,  6), laneY + 90, 48));
	const s3Positions = $derived(fanPositions(serviceX.s3,     Math.min(buckets.length,   6), laneY + 90, 48));

	const infraConnected = $derived(infra.filter((p) => p.status === 'connected').length);

	const hasData = $derived(
		gateways.length > 0 || functions.length > 0 || queues.length > 0 ||
		secrets.length > 0 || buckets.length > 0 || infra.length > 0
	);

	function handleGatewayKeydown(event: KeyboardEvent, apiId: string) {
		if (event.key === 'Enter' || event.key === ' ') {
			event.preventDefault();
			onGatewayClick(apiId);
		}
	}
</script>

<svg
	viewBox="0 0 {W} {H}"
	class="w-full"
	style={`min-width: ${canvasExpanded ? 820 : 480}px; max-height: ${canvasExpanded ? 620 : 380}px;`}
>
	<!-- Dot grid -->
	{#each Array(Math.floor(W / 40)) as _, ix (ix)}
		{#each Array(Math.floor(H / 40)) as _, iy (iy)}
			<circle cx={20 + ix * 40} cy={20 + iy * 40} r="0.6" class="fill-border" opacity="0.5" />
		{/each}
	{/each}

	{#if hasData}
		<!-- ── AWS service zone ─────────────────────────────────────── -->

		<!-- Fan lines: OpenStack endpoint → service headers -->
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.apigw}   y2={laneY - 16} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.lambda}  y2={laneY - 16} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.sqs}     y2={laneY - 16} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.secrets} y2={laneY - 16} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.s3}      y2={laneY - 16} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="4 3" />

		<!-- Fan lines: service header → individual resource nodes -->
		{#each gwPositions as pos}
			<line x1={serviceX.apigw}   y1={laneY + 16} x2={pos.x} y2={pos.y - 10} stroke="var(--color-red)"   stroke-width="0.75" opacity="0.35" />
		{/each}
		{#each fnPositions as pos}
			<line x1={serviceX.lambda}  y1={laneY + 16} x2={pos.x} y2={pos.y - 10} stroke="var(--color-accent)" stroke-width="0.75" opacity="0.3" />
		{/each}
		{#each qPositions as pos}
			<line x1={serviceX.sqs}     y1={laneY + 16} x2={pos.x} y2={pos.y - 10} stroke="var(--color-amber)" stroke-width="0.75" opacity="0.3" />
		{/each}
		{#each sPositions as pos}
			<line x1={serviceX.secrets} y1={laneY + 16} x2={pos.x} y2={pos.y - 10} stroke="var(--color-blue)"  stroke-width="0.75" opacity="0.3" />
		{/each}
		{#each s3Positions as pos}
			<line x1={serviceX.s3}      y1={laneY + 16} x2={pos.x} y2={pos.y - 10} stroke="var(--color-accent)" stroke-width="0.75" opacity="0.3" />
		{/each}

		<!-- OpenStack endpoint node -->
		<g>
			<rect x={endpoint.x - 56} y={endpoint.y - 16} width="112" height="32" rx="6" class="fill-bg-surface stroke-border-strong" stroke-width="1" />
			<circle cx={endpoint.x - 38} cy={endpoint.y} r="3" class="fill-accent" opacity="0.8">
				<animate attributeName="opacity" values="0.8;0.4;0.8" dur="2s" repeatCount="indefinite" />
			</circle>
			<text x={endpoint.x - 26} y={endpoint.y + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">OpenStack</text>
		</g>

		<!-- Service header badges -->
		<g>
			<rect x={serviceX.apigw - 52} y={laneY - 14} width="104" height="28" rx="5" class="fill-bg-surface" stroke="var(--color-red)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.apigw - 30} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">APIGW</text>
		</g>
		<g>
			<rect x={serviceX.lambda - 44} y={laneY - 14} width="88" height="28" rx="5" class="fill-bg-surface" stroke="var(--color-accent)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.lambda - 20} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">Lambda</text>
		</g>
		<g>
			<rect x={serviceX.sqs - 36} y={laneY - 14} width="72" height="28" rx="5" class="fill-bg-surface" stroke="var(--color-amber)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.sqs - 12} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">SQS</text>
		</g>
		<g>
			<rect x={serviceX.secrets - 44} y={laneY - 14} width="88" height="28" rx="5" class="fill-bg-surface" stroke="var(--color-blue)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.secrets - 20} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">Secrets</text>
		</g>
		<g>
			<rect x={serviceX.s3 - 36} y={laneY - 14} width="72" height="28" rx="5" class="fill-bg-surface" stroke="var(--color-accent)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.s3 - 8} y={laneY + 3.5} class="fill-text" font-size="9" font-family="var(--font-mono)">S3</text>
		</g>

		<!-- API Gateway resource nodes -->
		{#each gwPositions as pos, i (gateways[i]?.apiId ?? i)}
			{@const gw = gateways[i]}
			<g
				role="button"
				tabindex="0"
				aria-label={`Open API Gateway details for ${gw.name}`}
				class="cursor-pointer"
				onclick={() => onGatewayClick(gw.apiId)}
				onkeydown={(event: KeyboardEvent) => handleGatewayKeydown(event, gw.apiId)}
			>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4" class="fill-bg-overlay" stroke="var(--color-red)" stroke-width="0.75" opacity="0.7" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-red)" opacity="0.78" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">{gw.name.slice(0, 4)}</text>
			</g>
		{/each}

		<!-- Lambda Function resource nodes -->
		{#each fnPositions as pos, i (functions[i]?.name ?? i)}
			{@const fn = functions[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4" class="fill-bg-overlay" stroke={ledColorMap[stateColor(fn.state)]} stroke-width="0.75" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill={ledColorMap[stateColor(fn.state)]} />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">{fn.name.slice(0, 4)}</text>
			</g>
		{/each}

		<!-- SQS Queue resource nodes -->
		{#each qPositions as pos, i (queues[i]?.name ?? i)}
			{@const q = queues[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4" class="fill-bg-overlay" stroke="var(--color-amber)" stroke-width="0.75" opacity="0.6" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-amber)" opacity="0.7" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">{q.name.slice(0, 4)}</text>
			</g>
		{/each}

		<!-- Secrets Manager resource nodes -->
		{#each sPositions as pos, i (secrets[i]?.name ?? i)}
			{@const s = secrets[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4" class="fill-bg-overlay" stroke="var(--color-blue)" stroke-width="0.75" opacity="0.6" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-blue)" opacity="0.7" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">{s.name.slice(0, 4)}</text>
			</g>
		{/each}

		<!-- S3 Bucket resource nodes -->
		{#each s3Positions as pos, i (buckets[i]?.name ?? i)}
			{@const b = buckets[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4" class="fill-bg-overlay" stroke="var(--color-accent)" stroke-width="0.75" opacity="0.6" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-accent)" opacity="0.7" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-text-muted" font-size="7" font-family="var(--font-mono)">{b.name.slice(0, 4)}</text>
			</g>
		{/each}

		<!-- ── Local env zone ────────────────────────────────────────── -->

		<!-- Zone background tint -->
		<rect x={ZONE_X} y={0} width={W - ZONE_X} height={H} class="fill-bg-surface" opacity="0.25" />

		<!-- Vertical separator -->
		<line
			x1={ZONE_X} y1={14} x2={ZONE_X} y2={H - 14}
			stroke="var(--color-border)" stroke-width="0.75" stroke-dasharray="3 5" opacity="0.7"
		/>

		<!-- Zone header: label + connected count -->
		<text
			x={ZONE_CX} y={26}
			text-anchor="middle" font-size="7" font-family="var(--font-mono)"
			class="fill-text-faint" letter-spacing="0.8"
		>LOCAL ENV</text>

		{#if infra.length > 0}
			<!-- Connected indicator pill -->
			<rect
				x={ZONE_CX - 18} y={32} width={36} height={12} rx={6}
				fill={infraConnected > 0 ? 'var(--color-accent)' : 'var(--color-red)'}
				fill-opacity="0.12"
				stroke={infraConnected > 0 ? 'var(--color-accent)' : 'var(--color-red)'}
				stroke-width="0.5" opacity="0.6"
			/>
			<circle
				cx={ZONE_CX - 8} cy={38} r="2.5"
				fill={infraConnected > 0 ? 'var(--color-accent)' : 'var(--color-red)'}
				opacity="0.8"
			/>
			<text
				x={ZONE_CX - 1} y={41.5}
				text-anchor="start" font-size="7" font-family="var(--font-mono)"
				fill={infraConnected > 0 ? 'var(--color-accent)' : 'var(--color-red)'}
				opacity="0.85"
			>{infraConnected}/{infra.length}</text>

			<!-- Probe rows -->
			{#each infra as probe, i (probe.kind + probe.host + probe.port)}
				{@const rowY = ZONE_ROW_START_Y + i * ZONE_ROW_H}
				{@const color = probeColor(probe)}
				{@const isHTTP = probe.kind === 'http' || probe.kind === 'https'}
				<g>
					<!-- Row background -->
					<rect
						x={ZONE_X + 6} y={rowY - 11}
						width={W - ZONE_X - 10} height={22}
						rx={3}
						fill={color} fill-opacity="0.05"
						stroke={color} stroke-width="0.5"
						stroke-dasharray={isHTTP ? '3 2' : 'none'}
						opacity="0.5"
					/>

					<!-- Status dot (animated pulse for HTTP connected) -->
					{#if isHTTP && probe.status === 'connected'}
						<circle cx={ZONE_X + 18} cy={rowY} r="5" fill={color} opacity="0.12">
							<animate attributeName="r" values="5;8;5" dur="2.5s" repeatCount="indefinite" />
							<animate attributeName="opacity" values="0.12;0;0.12" dur="2.5s" repeatCount="indefinite" />
						</circle>
					{/if}
					<circle cx={ZONE_X + 18} cy={rowY} r="3.5" fill={color} opacity="0.85" />

					<!-- Kind tag for HTTP probes -->
					{#if isHTTP}
						<rect x={ZONE_X + 26} y={rowY - 6} width={18} height={11} rx={2}
							fill={color} fill-opacity="0.1" stroke={color} stroke-width="0.4" opacity="0.6" />
						<text x={ZONE_X + 35} y={rowY + 2} text-anchor="middle"
							font-size="6" font-family="var(--font-mono)" fill={color} opacity="0.9">http</text>
					{/if}

					<!-- Probe name -->
					<text
						x={isHTTP ? ZONE_X + 48 : ZONE_X + 28}
						y={rowY + 3.5}
						font-size="8.5" font-family="var(--font-mono)"
						class="fill-text"
					>{probe.name.slice(0, isHTTP ? 11 : 14)}</text>

					<!-- Latency or error status -->
					{#if probe.status === 'connected'}
						<text
							x={W - 8} y={rowY + 3.5}
							text-anchor="end" font-size="7.5" font-family="var(--font-mono)"
							class="fill-text-faint"
						>{Math.round(probe.latencyMs)}ms</text>
					{:else}
						<text
							x={W - 8} y={rowY + 3.5}
							text-anchor="end" font-size="7.5" font-family="var(--font-mono)"
							fill="var(--color-red)" opacity="0.6"
						>{probe.status}</text>
					{/if}
				</g>
			{/each}
		{:else}
			<!-- Empty state -->
			<text
				x={ZONE_CX} y={H / 2 - 8}
				text-anchor="middle" font-size="8" font-family="var(--font-mono)"
				class="fill-text-faint"
			>no services</text>
			<text
				x={ZONE_CX} y={H / 2 + 8}
				text-anchor="middle" font-size="7" font-family="var(--font-mono)"
				class="fill-text-faint" opacity="0.5"
			>configure in settings</text>
		{/if}
	{:else}
		<text x={CX} y={H / 2} text-anchor="middle" class="fill-text-faint" font-size="11" font-family="var(--font-mono)">
			{dashboard.loading ? 'Connecting...' : 'No data'}
		</text>
	{/if}
</svg>
