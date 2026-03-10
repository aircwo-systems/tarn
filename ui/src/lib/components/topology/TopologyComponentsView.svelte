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
		onGatewayClick = (_id: string) => {},
		onNavigate = (_tab: string) => {}
	}: {
		gateways?: GatewaySummary[];
		functions?: FunctionSummary[];
		queues?: QueueSummary[];
		secrets?: SecretSummary[];
		buckets?: BucketSummary[];
		infra?: InfraProbe[];
		canvasExpanded?: boolean;
		onGatewayClick?: (id: string) => void;
		onNavigate?: (tab: string) => void;
	} = $props();

	const dashboard = getDashboard();

	// --- Canvas geometry ---
	// Wider canvas gives room for large infra (multiple lambdas, queues, secrets, buckets)
	const W = 1400;
	const H = 560;
	const CX = W / 2;
	const endpoint = { x: CX, y: 58 };
	const laneY = 200;

	// AWS service columns — 220px apart, giving each column 110px of node-fan space each side
	const serviceX = { apigw: 90, lambda: 310, sqs: 530, secrets: 750, s3: 970 };

	// Two-row resource layout
	const ROW1_Y = laneY + 130;  // 330
	const ROW2_Y = laneY + 245;  // 445
	const ROW1_MAX = 5;          // up to 5 per row = 10 total per service
	const NODE_SPACING = 46;

	// Local env zone — right panel (wider for more probe info)
	const ZONE_X = 1090;
	const ZONE_CX = ZONE_X + (W - ZONE_X) / 2;   // 1245
	const ZONE_ROW_START_Y = 80;
	const ZONE_ROW_H = 46;       // taller rows for readability

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

	// Fan out positions — single row helper
	function fanRow(cx: number, count: number, y: number): { x: number; y: number }[] {
		if (count === 0) return [];
		const total = (count - 1) * NODE_SPACING;
		const startX = cx - total / 2;
		return Array.from({ length: count }, (_, i) => ({ x: startX + i * NODE_SPACING, y }));
	}

	// Two-row fan: up to ROW1_MAX in row1, remaining in row2 (max ROW1_MAX each row = 10 total)
	function fanPositions(cx: number, count: number): { x: number; y: number }[] {
		const r1 = Math.min(count, ROW1_MAX);
		const r2 = Math.min(count - r1, ROW1_MAX);
		return [...fanRow(cx, r1, ROW1_Y), ...fanRow(cx, r2, ROW2_Y)];
	}

	const MAX_NODES = ROW1_MAX * 2;
	const gwPositions = $derived(fanPositions(serviceX.apigw,   Math.min(gateways.length,  MAX_NODES)));
	const fnPositions = $derived(fanPositions(serviceX.lambda,  Math.min(functions.length, MAX_NODES)));
	const qPositions  = $derived(fanPositions(serviceX.sqs,     Math.min(queues.length,    MAX_NODES)));
	const sPositions  = $derived(fanPositions(serviceX.secrets, Math.min(secrets.length,   MAX_NODES)));
	const s3Positions = $derived(fanPositions(serviceX.s3,      Math.min(buckets.length,   MAX_NODES)));

	// Overflow counts (resources beyond what's shown)
	const gwOverflow  = $derived(Math.max(0, gateways.length  - MAX_NODES));
	const fnOverflow  = $derived(Math.max(0, functions.length - MAX_NODES));
	const qOverflow   = $derived(Math.max(0, queues.length    - MAX_NODES));
	const sOverflow   = $derived(Math.max(0, secrets.length   - MAX_NODES));
	const s3Overflow  = $derived(Math.max(0, buckets.length   - MAX_NODES));

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

	function handleNavKeydown(event: KeyboardEvent, tab: string) {
		if (event.key === 'Enter' || event.key === ' ') {
			event.preventDefault();
			onNavigate(tab);
		}
	}
</script>

<svg
	viewBox="0 0 {W} {H}"
	class="w-full h-full"
	preserveAspectRatio="xMidYMin meet"
	style={`min-width: ${canvasExpanded ? 1100 : 700}px;`}
>
	<!-- Canvas background -->
	<rect x={0} y={0} width={W} height={H} class="fill-bg" />

	<!-- Dot grid -->
	{#each Array(Math.floor(W / 44)) as _, ix (ix)}
		{#each Array(Math.floor(H / 44)) as _, iy (iy)}
			<circle cx={22 + ix * 44} cy={22 + iy * 44} r="0.65" class="fill-border" opacity="0.45" />
		{/each}
	{/each}

	{#if hasData}
		<!-- ── AWS service zone ─────────────────────────────────────── -->

		<!-- Fan lines: OpenStack endpoint → service headers (longer sweeping lines) -->
		<line x1={endpoint.x} y1={endpoint.y + 17} x2={serviceX.apigw}   y2={laneY - 15} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="5 4" opacity="0.8" />
		<line x1={endpoint.x} y1={endpoint.y + 17} x2={serviceX.lambda}  y2={laneY - 15} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="5 4" opacity="0.8" />
		<line x1={endpoint.x} y1={endpoint.y + 17} x2={serviceX.sqs}     y2={laneY - 15} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="5 4" opacity="0.8" />
		<line x1={endpoint.x} y1={endpoint.y + 17} x2={serviceX.secrets} y2={laneY - 15} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="5 4" opacity="0.8" />
		<line x1={endpoint.x} y1={endpoint.y + 17} x2={serviceX.s3}      y2={laneY - 15} stroke="var(--color-border-strong)" stroke-width="1" stroke-dasharray="5 4" opacity="0.8" />

		<!-- Fan lines: service header → individual resource nodes (row1 + row2) -->
		{#each gwPositions as pos}
			<line x1={serviceX.apigw}   y1={laneY + 15} x2={pos.x} y2={pos.y - 11} stroke="var(--color-red)"    stroke-width="0.7" opacity={pos.y > ROW1_Y ? 0.2 : 0.32} />
		{/each}
		{#each fnPositions as pos}
			<line x1={serviceX.lambda}  y1={laneY + 15} x2={pos.x} y2={pos.y - 11} stroke="var(--color-accent)" stroke-width="0.7" opacity={pos.y > ROW1_Y ? 0.18 : 0.28} />
		{/each}
		{#each qPositions as pos}
			<line x1={serviceX.sqs}     y1={laneY + 15} x2={pos.x} y2={pos.y - 11} stroke="var(--color-amber)"  stroke-width="0.7" opacity={pos.y > ROW1_Y ? 0.18 : 0.28} />
		{/each}
		{#each sPositions as pos}
			<line x1={serviceX.secrets} y1={laneY + 15} x2={pos.x} y2={pos.y - 11} stroke="var(--color-blue)"   stroke-width="0.7" opacity={pos.y > ROW1_Y ? 0.18 : 0.28} />
		{/each}
		{#each s3Positions as pos}
			<line x1={serviceX.s3}      y1={laneY + 15} x2={pos.x} y2={pos.y - 11} stroke="var(--color-accent)" stroke-width="0.7" opacity={pos.y > ROW1_Y ? 0.18 : 0.28} />
		{/each}

		<!-- OpenStack endpoint node -->
		<g>
			<rect x={endpoint.x - 62} y={endpoint.y - 17} width="124" height="34" rx="6" class="fill-bg-surface stroke-border-strong" stroke-width="1" />
			<circle cx={endpoint.x - 42} cy={endpoint.y} r="3.5" class="fill-accent" opacity="0.85">
				<animate attributeName="opacity" values="0.85;0.4;0.85" dur="2s" repeatCount="indefinite" />
			</circle>
			<text x={endpoint.x - 28} y={endpoint.y + 4} class="fill-text" font-size="10" font-family="var(--font-mono)">OpenStack</text>
		</g>

		<!-- Service header badges -->
		<!-- APIGW -->
		<g>
			<rect x={serviceX.apigw - 56} y={laneY - 15} width="112" height="30" rx="5" class="fill-bg-surface" stroke="var(--color-red)" stroke-width="1" opacity="0.85" />
			<text x={serviceX.apigw} y={laneY + 4.5} text-anchor="middle" class="fill-text" font-size="9.5" font-family="var(--font-mono)">APIGW</text>
		</g>
		{#if gwOverflow > 0}
			<text x={serviceX.apigw + 60} y={laneY + 4.5} class="fill-text-faint" font-size="8" font-family="var(--font-mono)">+{gwOverflow}</text>
		{/if}
		<!-- Lambda -->
		<g>
			<rect x={serviceX.lambda - 52} y={laneY - 15} width="104" height="30" rx="5" class="fill-bg-surface" stroke="var(--color-accent)" stroke-width="1" opacity="0.85" />
			<text x={serviceX.lambda} y={laneY + 4.5} text-anchor="middle" class="fill-text" font-size="9.5" font-family="var(--font-mono)">Lambda</text>
		</g>
		{#if fnOverflow > 0}
			<text x={serviceX.lambda + 57} y={laneY + 4.5} class="fill-text-faint" font-size="8" font-family="var(--font-mono)">+{fnOverflow}</text>
		{/if}
		<!-- SQS -->
		<g>
			<rect x={serviceX.sqs - 42} y={laneY - 15} width="84" height="30" rx="5" class="fill-bg-surface" stroke="var(--color-amber)" stroke-width="1" opacity="0.85" />
			<text x={serviceX.sqs} y={laneY + 4.5} text-anchor="middle" class="fill-text" font-size="9.5" font-family="var(--font-mono)">SQS</text>
		</g>
		{#if qOverflow > 0}
			<text x={serviceX.sqs + 47} y={laneY + 4.5} class="fill-text-faint" font-size="8" font-family="var(--font-mono)">+{qOverflow}</text>
		{/if}
		<!-- Secrets -->
		<g>
			<rect x={serviceX.secrets - 52} y={laneY - 15} width="104" height="30" rx="5" class="fill-bg-surface" stroke="var(--color-blue)" stroke-width="1" opacity="0.85" />
			<text x={serviceX.secrets} y={laneY + 4.5} text-anchor="middle" class="fill-text" font-size="9.5" font-family="var(--font-mono)">Secrets</text>
		</g>
		{#if sOverflow > 0}
			<text x={serviceX.secrets + 57} y={laneY + 4.5} class="fill-text-faint" font-size="8" font-family="var(--font-mono)">+{sOverflow}</text>
		{/if}
		<!-- S3 -->
		<g>
			<rect x={serviceX.s3 - 42} y={laneY - 15} width="84" height="30" rx="5" class="fill-bg-surface" stroke="var(--color-accent)" stroke-width="1" opacity="0.85" />
			<text x={serviceX.s3} y={laneY + 4.5} text-anchor="middle" class="fill-text" font-size="9.5" font-family="var(--font-mono)">S3</text>
		</g>
		{#if s3Overflow > 0}
			<text x={serviceX.s3 + 47} y={laneY + 4.5} class="fill-text-faint" font-size="8" font-family="var(--font-mono)">+{s3Overflow}</text>
		{/if}

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
				<rect x={pos.x - 20} y={pos.y - 11} width="40" height="22" rx="4" class="fill-bg-overlay" stroke="var(--color-red)" stroke-width="0.8" opacity="0.75" />
				<circle cx={pos.x - 10} cy={pos.y} r="2.5" fill="var(--color-red)" opacity="0.8" />
				<text x={pos.x - 3} y={pos.y + 3.5} class="fill-text-muted" font-size="7.5" font-family="var(--font-mono)">{gw.name.slice(0, 5)}</text>
			</g>
		{/each}

		<!-- Lambda Function resource nodes -->
		{#each fnPositions as pos, i (functions[i]?.name ?? i)}
			{@const fn = functions[i]}
			<g
				role="button"
				tabindex="0"
				aria-label={`View Lambda function ${fn.name}`}
				class="cursor-pointer"
				onclick={() => onNavigate('functions')}
				onkeydown={(event: KeyboardEvent) => handleNavKeydown(event, 'functions')}
			>
				<rect x={pos.x - 20} y={pos.y - 11} width="40" height="22" rx="4" class="fill-bg-overlay" stroke={ledColorMap[stateColor(fn.state)]} stroke-width="0.8" />
				<circle cx={pos.x - 10} cy={pos.y} r="2.5" fill={ledColorMap[stateColor(fn.state)]} />
				<text x={pos.x - 3} y={pos.y + 3.5} class="fill-text-muted" font-size="7.5" font-family="var(--font-mono)">{fn.name.slice(0, 5)}</text>
			</g>
		{/each}

		<!-- SQS Queue resource nodes -->
		{#each qPositions as pos, i (queues[i]?.name ?? i)}
			{@const q = queues[i]}
			<g
				role="button"
				tabindex="0"
				aria-label={`View SQS queue ${q.name}`}
				class="cursor-pointer"
				onclick={() => onNavigate('queues')}
				onkeydown={(event: KeyboardEvent) => handleNavKeydown(event, 'queues')}
			>
				<rect x={pos.x - 20} y={pos.y - 11} width="40" height="22" rx="4" class="fill-bg-overlay" stroke="var(--color-amber)" stroke-width="0.8" opacity="0.65" />
				<circle cx={pos.x - 10} cy={pos.y} r="2.5" fill="var(--color-amber)" opacity="0.75" />
				<text x={pos.x - 3} y={pos.y + 3.5} class="fill-text-muted" font-size="7.5" font-family="var(--font-mono)">{q.name.slice(0, 5)}</text>
			</g>
		{/each}

		<!-- Secrets Manager resource nodes -->
		{#each sPositions as pos, i (secrets[i]?.name ?? i)}
			{@const s = secrets[i]}
			<g
				role="button"
				tabindex="0"
				aria-label={`View secret ${s.name}`}
				class="cursor-pointer"
				onclick={() => onNavigate('secrets')}
				onkeydown={(event: KeyboardEvent) => handleNavKeydown(event, 'secrets')}
			>
				<rect x={pos.x - 20} y={pos.y - 11} width="40" height="22" rx="4" class="fill-bg-overlay" stroke="var(--color-blue)" stroke-width="0.8" opacity="0.65" />
				<circle cx={pos.x - 10} cy={pos.y} r="2.5" fill="var(--color-blue)" opacity="0.75" />
				<text x={pos.x - 3} y={pos.y + 3.5} class="fill-text-muted" font-size="7.5" font-family="var(--font-mono)">{s.name.slice(0, 5)}</text>
			</g>
		{/each}

		<!-- S3 Bucket resource nodes -->
		{#each s3Positions as pos, i (buckets[i]?.name ?? i)}
			{@const b = buckets[i]}
			<g
				role="button"
				tabindex="0"
				aria-label={`View S3 bucket ${b.name}`}
				class="cursor-pointer"
				onclick={() => onNavigate('storage')}
				onkeydown={(event: KeyboardEvent) => handleNavKeydown(event, 'storage')}
			>
				<rect x={pos.x - 20} y={pos.y - 11} width="40" height="22" rx="4" class="fill-bg-overlay" stroke="var(--color-accent)" stroke-width="0.8" opacity="0.65" />
				<circle cx={pos.x - 10} cy={pos.y} r="2.5" fill="var(--color-accent)" opacity="0.75" />
				<text x={pos.x - 3} y={pos.y + 3.5} class="fill-text-muted" font-size="7.5" font-family="var(--font-mono)">{b.name.slice(0, 5)}</text>
			</g>
		{/each}

		<!-- ── Local env zone ────────────────────────────────────────── -->

		<!-- Zone background tint (darker, more distinct) -->
		<rect x={ZONE_X} y={0} width={W - ZONE_X} height={H} class="fill-bg-surface" opacity="0.3" />

		<!-- Vertical separator -->
		<line
			x1={ZONE_X} y1={16} x2={ZONE_X} y2={H - 16}
			stroke="var(--color-border)" stroke-width="0.75" stroke-dasharray="3 6" opacity="0.6"
		/>

		<!-- Zone header -->
		<text
			x={ZONE_CX} y={30}
			text-anchor="middle" font-size="7.5" font-family="var(--font-mono)"
			class="fill-text-faint" letter-spacing="1.2"
		>LOCAL ENV</text>

		{#if infra.length > 0}
			<!-- Connected indicator -->
			<rect
				x={ZONE_CX - 22} y={36} width={44} height={14} rx={7}
				fill={infraConnected > 0 ? 'var(--color-accent)' : 'var(--color-red)'}
				fill-opacity="0.12"
				stroke={infraConnected > 0 ? 'var(--color-accent)' : 'var(--color-red)'}
				stroke-width="0.5" opacity="0.6"
			/>
			<circle
				cx={ZONE_CX - 9} cy={43} r="3"
				fill={infraConnected > 0 ? 'var(--color-accent)' : 'var(--color-red)'}
				opacity="0.85"
			/>
			<text
				x={ZONE_CX - 1} y={47}
				text-anchor="start" font-size="7.5" font-family="var(--font-mono)"
				fill={infraConnected > 0 ? 'var(--color-accent)' : 'var(--color-red)'}
				opacity="0.9"
			>{infraConnected}/{infra.length}</text>

			<!-- Probe rows (up to 9, with taller rows for readability) -->
			{#each infra as probe, i (probe.kind + probe.host + probe.port)}
				{@const rowY = ZONE_ROW_START_Y + i * ZONE_ROW_H}
				{@const color = probeColor(probe)}
				{@const isHTTP = probe.kind === 'http' || probe.kind === 'https'}
				<g>
					<rect
						x={ZONE_X + 8} y={rowY - 14}
						width={W - ZONE_X - 14} height={28}
						rx={4}
						fill={color} fill-opacity="0.05"
						stroke={color} stroke-width="0.5"
						stroke-dasharray={isHTTP ? '3 2' : 'none'}
						opacity="0.55"
					/>

					{#if isHTTP && probe.status === 'connected'}
						<circle cx={ZONE_X + 22} cy={rowY} r="6" fill={color} opacity="0.1">
							<animate attributeName="r" values="6;10;6" dur="2.5s" repeatCount="indefinite" />
							<animate attributeName="opacity" values="0.1;0;0.1" dur="2.5s" repeatCount="indefinite" />
						</circle>
					{/if}
					<circle cx={ZONE_X + 22} cy={rowY} r="4" fill={color} opacity="0.88" />

					{#if isHTTP}
						<rect x={ZONE_X + 32} y={rowY - 7} width={22} height={13} rx={2}
							fill={color} fill-opacity="0.1" stroke={color} stroke-width="0.4" opacity="0.6" />
						<text x={ZONE_X + 43} y={rowY + 2.5} text-anchor="middle"
							font-size="6.5" font-family="var(--font-mono)" fill={color} opacity="0.9">http</text>
					{/if}

					<text
						x={isHTTP ? ZONE_X + 60 : ZONE_X + 34}
						y={rowY + 4}
						font-size="9" font-family="var(--font-mono)"
						class="fill-text"
					>{probe.name.slice(0, isHTTP ? 15 : 18)}</text>

					{#if probe.status === 'connected'}
						<text
							x={W - 10} y={rowY + 4}
							text-anchor="end" font-size="8" font-family="var(--font-mono)"
							class="fill-text-faint"
						>{Math.round(probe.latencyMs)}ms</text>
					{:else}
						<text
							x={W - 10} y={rowY + 4}
							text-anchor="end" font-size="8" font-family="var(--font-mono)"
							fill="var(--color-red)" opacity="0.65"
						>{probe.status}</text>
					{/if}
				</g>
			{/each}
		{:else}
			<text
				x={ZONE_CX} y={H / 2 - 10}
				text-anchor="middle" font-size="9" font-family="var(--font-mono)"
				class="fill-text-faint"
			>no services</text>
			<text
				x={ZONE_CX} y={H / 2 + 10}
				text-anchor="middle" font-size="7.5" font-family="var(--font-mono)"
				class="fill-text-faint" opacity="0.5"
			>configure in settings</text>
		{/if}
	{:else}
		<text x={CX} y={H / 2} text-anchor="middle" class="fill-text-faint" font-size="12" font-family="var(--font-mono)">
			{dashboard.loading ? 'Connecting...' : 'No data'}
		</text>
	{/if}
</svg>
