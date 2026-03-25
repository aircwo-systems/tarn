<script lang="ts">
	let {
		gateways,
		functions,
		queues,
		secrets,
		buckets,
		infra,
		gwPositions,
		fnPositions,
		qPositions,
		sPositions,
		s3Positions,
		infraPositions,
		hasData,
		endpoint,
		serviceX,
		laneY,
		W,
		H,
		CX,
		openGateway,
		handleGatewayKeydown,
		selectedGatewayId,
		stateColor,
		ledColorMap,
		canvasExpanded,
	}: any = $props();
</script>

<svg
	viewBox={`0 0 ${W} ${H}`}
	class="w-full"
	style={`min-width: ${canvasExpanded ? 820 : 480}px; max-height: ${canvasExpanded ? 620 : 380}px;`}
>
	{#each Array(Math.floor(W / 40)) as _, ix}
		{#each Array(Math.floor(H / 40)) as _, iy}
			<circle cx={20 + ix * 40} cy={20 + iy * 40} r="0.6" class="fill-[var(--color-border)]" opacity="0.5" />
		{/each}
	{/each}

	{#if hasData}
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.apigw} y2={laneY - 16}
			stroke="var(--color-border)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.lambda} y2={laneY - 16}
			stroke="var(--color-border)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.sqs} y2={laneY - 16}
			stroke="var(--color-border)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.secrets} y2={laneY - 16}
			stroke="var(--color-border)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.s3} y2={laneY - 16}
			stroke="var(--color-border)" stroke-width="1" stroke-dasharray="4 3" />
		<line x1={endpoint.x} y1={endpoint.y + 16} x2={serviceX.infra} y2={laneY - 16}
			stroke="var(--color-border)" stroke-width="1" stroke-dasharray="4 3" />

		{#each gwPositions as pos}
			<line x1={serviceX.apigw} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
				stroke="var(--color-destructive)" stroke-width="0.75" opacity="0.35" />
		{/each}
		{#each fnPositions as pos}
			<line x1={serviceX.lambda} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
				stroke="var(--color-primary)" stroke-width="0.75" opacity="0.3" />
		{/each}
		{#each qPositions as pos}
			<line x1={serviceX.sqs} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
				stroke="var(--color-chart-4)" stroke-width="0.75" opacity="0.3" />
		{/each}
		{#each sPositions as pos}
			<line x1={serviceX.secrets} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
				stroke="var(--color-chart-2)" stroke-width="0.75" opacity="0.3" />
		{/each}
		{#each s3Positions as pos}
			<line x1={serviceX.s3} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
				stroke="var(--color-primary)" stroke-width="0.75" opacity="0.3" />
		{/each}
		{#each infraPositions as pos}
			<line x1={serviceX.infra} y1={laneY + 16} x2={pos.x} y2={pos.y - 10}
				stroke="var(--color-chart-5, var(--color-muted-foreground))" stroke-width="0.75" opacity="0.3" />
		{/each}

		<g>
			<rect x={endpoint.x - 56} y={endpoint.y - 16} width="112" height="32" rx="6"
				class="fill-muted stroke-border" stroke-width="1" />
			<circle cx={endpoint.x - 38} cy={endpoint.y} r="3" class="fill-accent" opacity="0.8">
				<animate attributeName="opacity" values="0.8;0.4;0.8" dur="2s" repeatCount="indefinite" />
			</circle>
			<text x={endpoint.x - 26} y={endpoint.y + 3.5} class="fill-foreground" font-size="9" font-family="var(--font-mono)">
				Tarn
			</text>
		</g>

		<g>
			<rect x={serviceX.apigw - 52} y={laneY - 14} width="104" height="28" rx="5"
				class="fill-[var(--color-muted)]" stroke="var(--color-destructive)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.apigw - 30} y={laneY + 3.5} class="fill-foreground" font-size="9" font-family="var(--font-mono)">
				APIGW
			</text>
		</g>

		<g>
			<rect x={serviceX.lambda - 44} y={laneY - 14} width="88" height="28" rx="5"
				class="fill-[var(--color-muted)]" stroke="var(--color-primary)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.lambda - 20} y={laneY + 3.5} class="fill-foreground" font-size="9" font-family="var(--font-mono)">
				Lambda
			</text>
		</g>

		<g>
			<rect x={serviceX.sqs - 36} y={laneY - 14} width="72" height="28" rx="5"
				class="fill-[var(--color-muted)]" stroke="var(--color-chart-4)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.sqs - 12} y={laneY + 3.5} class="fill-foreground" font-size="9" font-family="var(--font-mono)">
				SQS
			</text>
		</g>

		<g>
			<rect x={serviceX.secrets - 44} y={laneY - 14} width="88" height="28" rx="5"
				class="fill-[var(--color-muted)]" stroke="var(--color-chart-2)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.secrets - 20} y={laneY + 3.5} class="fill-foreground" font-size="9" font-family="var(--font-mono)">
				Secrets
			</text>
		</g>

		<g>
			<rect x={serviceX.s3 - 36} y={laneY - 14} width="72" height="28" rx="5"
				class="fill-[var(--color-muted)]" stroke="var(--color-primary)" stroke-width="1" opacity="0.8" />
			<text x={serviceX.s3 - 8} y={laneY + 3.5} class="fill-foreground" font-size="9" font-family="var(--font-mono)">
				S3
			</text>
		</g>

		<g>
			<rect x={serviceX.infra - 44} y={laneY - 14} width="88" height="28" rx="5"
				class="fill-muted stroke-muted-foreground" stroke-width="1" opacity="0.8" />
			<text x={serviceX.infra - 16} y={laneY + 3.5} class="fill-foreground" font-size="9" font-family="var(--font-mono)">
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
				onkeydown={(event) => handleGatewayKeydown(event, gw.apiId)}
			>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
					class="fill-[var(--color-popover)]" stroke="var(--color-destructive)" stroke-width="0.75" opacity="0.7" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-destructive)" opacity="0.78" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-[var(--color-muted-foreground)]" font-size="7" font-family="var(--font-mono)">
					{gw.name.length > 4 ? gw.name.slice(0, 4) : gw.name}
				</text>
			</g>
		{/each}

		{#each fnPositions as pos, i}
			{@const fn = functions[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
					class="fill-[var(--color-popover)]" stroke={ledColorMap[stateColor(fn.state)]} stroke-width="0.75" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill={ledColorMap[stateColor(fn.state)]} />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-[var(--color-muted-foreground)]" font-size="7" font-family="var(--font-mono)">
					{fn.name.length > 4 ? fn.name.slice(0, 4) : fn.name}
				</text>
			</g>
		{/each}

		{#each qPositions as pos, i}
			{@const q = queues[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
					class="fill-[var(--color-popover)]" stroke="var(--color-chart-4)" stroke-width="0.75" opacity="0.6" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-chart-4)" opacity="0.7" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-[var(--color-muted-foreground)]" font-size="7" font-family="var(--font-mono)">
					{q.name.length > 4 ? q.name.slice(0, 4) : q.name}
				</text>
			</g>
		{/each}

		{#each sPositions as pos, i}
			{@const s = secrets[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
					class="fill-[var(--color-popover)]" stroke="var(--color-chart-2)" stroke-width="0.75" opacity="0.6" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-chart-2)" opacity="0.7" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-[var(--color-muted-foreground)]" font-size="7" font-family="var(--font-mono)">
					{s.name.length > 4 ? s.name.slice(0, 4) : s.name}
				</text>
			</g>
		{/each}

		{#each s3Positions as pos, i}
			{@const b = buckets[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
					class="fill-[var(--color-popover)]" stroke="var(--color-primary)" stroke-width="0.75" opacity="0.6" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill="var(--color-primary)" opacity="0.7" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-[var(--color-muted-foreground)]" font-size="7" font-family="var(--font-mono)">
					{b.name.length > 4 ? b.name.slice(0, 4) : b.name}
				</text>
			</g>
		{/each}

		{#each infraPositions as pos, i}
			{@const probe = infra[i]}
			<g>
				<rect x={pos.x - 18} y={pos.y - 10} width="36" height="20" rx="4"
					class="fill-[var(--color-popover)]" stroke={probe.status === 'connected' ? 'var(--color-primary)' : 'var(--color-destructive)'} stroke-width="0.75" opacity="0.7" />
				<circle cx={pos.x - 9} cy={pos.y} r="2" fill={probe.status === 'connected' ? 'var(--color-primary)' : 'var(--color-destructive)'} opacity="0.78" />
				<text x={pos.x - 3} y={pos.y + 3} class="fill-[var(--color-muted-foreground)]" font-size="7" font-family="var(--font-mono)">
					{probe.name.length > 4 ? probe.name.slice(0, 4) : probe.name}
				</text>
			</g>
		{/each}

	{:else}
		<text x={CX} y={H / 2} text-anchor="middle" class="fill-foreground-faint" font-size="11" font-family="var(--font-mono)">
			Connecting...
		</text>
	{/if}
</svg>
