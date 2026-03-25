import type { GatewaySummary, RouteDetail, ChaosRound, ChaosRoundExample } from './types';

export function slugify(value: string): string {
	return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'gateway';
}

export function postmanPath(path: string): string {
	if (!path || path === '/') return '';
	return path.replace(/\{([a-zA-Z0-9_]+)\+?\}/g, '{{$1}}');
}

export function routeVariableNames(path: string): string[] {
	const names = new Set<string>();
	for (const m of path.matchAll(/\{([a-zA-Z0-9_]+)\+?\}/g)) {
		if (m[1]) names.add(m[1]);
	}
	return Array.from(names);
}

export function normalizeBaseUrl(raw: string): string {
	try {
		const parsed = new URL(raw);
		if (['0.0.0.0', '::', '[::]'].includes(parsed.hostname)) parsed.hostname = '127.0.0.1';
		return parsed.origin;
	} catch {
		return raw;
	}
}

export function normalizeInvokeUrl(raw: string): string {
	try {
		const parsed = new URL(raw);
		if (['0.0.0.0', '::', '[::]'].includes(parsed.hostname)) parsed.hostname = '127.0.0.1';
		return parsed.href.replace(/\/$/, '');
	} catch {
		return raw;
	}
}

export function normalizeTemplate(t: string): string {
	return t.split('\n').map((l) => l.trim()).filter((l) => l.length > 0).join('\n');
}

export function parseTarget(target: string): { kind: string; name: string } {
	if (target.startsWith('sqs:')) return { kind: 'SQS', name: target.slice(4) };
	if (target.startsWith('lambda:')) return { kind: 'λ', name: target.slice(7) };
	return { kind: '', name: target };
}

function buildItemBody(detail: RouteDetail): { mode: string; raw: string; options: unknown } | undefined {
	const m = (detail.method ?? '').toUpperCase();
	if (!['POST', 'PUT', 'PATCH'].includes(m)) return undefined;
	let raw = '{}';
	// Priority 1: real event from the Lambda's events/ folder
	if (detail.bodyExample !== undefined && detail.bodyExample !== null) {
		raw = JSON.stringify(detail.bodyExample, null, 2);
	// Priority 2: v1 VTL request template (AWS/SQS integrations)
	} else if (detail.requestTemplates) {
		const tmpl = detail.requestTemplates['application/json'];
		if (tmpl?.trim()) raw = normalizeTemplate(tmpl);
	}
	return { mode: 'raw', raw, options: { raw: { language: 'json' } } };
}

function buildItemHeaders(detail: RouteDetail): Array<{ key: string; value: string; type: string; description?: string }> {
	const m = (detail.method ?? '').toUpperCase();
	const headers: Array<{ key: string; value: string; type: string; description?: string }> = [];

	if (['POST', 'PUT', 'PATCH'].includes(m)) {
		headers.push({ key: 'Content-Type', value: 'application/json', type: 'text' });
	}

	// v1 method-level declared headers (method.request.header.X-Foo → required bool)
	if (detail.methodRequestParams) {
		for (const [k, required] of Object.entries(detail.methodRequestParams)) {
			const match = k.match(/^method\.request\.header\.(.+)$/i);
			if (match?.[1]) {
				const name = match[1];
				if (!headers.some((h) => h.key.toLowerCase() === name.toLowerCase())) {
					headers.push({
						key: name,
						value: '',
						type: 'text',
						description: required ? 'Required' : 'Optional'
					});
				}
			}
		}
	}

	// v2 integration-level header overrides (overwrite.header.X-Foo / append.header.X-Foo)
	if (detail.requestParameters) {
		for (const [k, v] of Object.entries(detail.requestParameters)) {
			const match = k.match(/(?:overwrite|append)\.header\.(.+)/i);
			if (match?.[1] && !headers.some((h) => h.key.toLowerCase() === match[1].toLowerCase())) {
				headers.push({ key: match[1], value: v, type: 'text' });
			}
		}
	}

	return headers;
}

function routeKeyParts(routeKey: string): { method: string; path: string } {
	if (routeKey === '$default') return { method: '$default', path: '/' };
	const i = routeKey.indexOf(' ');
	if (i === -1) return { method: 'ANY', path: routeKey };
	return { method: routeKey.slice(0, i), path: routeKey.slice(i + 1) || '/' };
}

export function collectionVariables(gw: GatewaySummary): Array<{ key: string; value: string; type?: string }> {
	const vars = new Map<string, { key: string; value: string; type?: string }>();
	vars.set('baseUrl', { key: 'baseUrl', value: normalizeBaseUrl(gw.invokeUrl), type: 'string' });
	vars.set('apiId', { key: 'apiId', value: gw.apiId, type: 'string' });
	vars.set('stage', { key: 'stage', value: gw.defaultStage, type: 'string' });
	vars.set('invokeBase', { key: 'invokeBase', value: normalizeInvokeUrl(gw.invokeUrl), type: 'string' });
	const details = gw.routeDetails ?? [];
	if (details.length > 0) {
		for (const d of details) {
			for (const v of routeVariableNames(d.path ?? '')) {
				if (!vars.has(v)) vars.set(v, { key: v, value: '', type: 'string' });
			}
			if (d.method === '$default' && !vars.has('defaultPath')) {
				vars.set('defaultPath', { key: 'defaultPath', value: '', type: 'string' });
			}
		}
	} else {
		for (const rk of gw.routeKeys ?? []) {
			const { method, path } = routeKeyParts(rk);
			for (const v of routeVariableNames(path)) {
				if (!vars.has(v)) vars.set(v, { key: v, value: '', type: 'string' });
			}
			if (method === '$default' && !vars.has('defaultPath')) {
				vars.set('defaultPath', { key: 'defaultPath', value: '', type: 'string' });
			}
		}
	}
	return Array.from(vars.values());
}

function statusText(code: number): string {
	if (code >= 500) return 'Internal Server Error';
	if (code >= 400) return 'Bad Request';
	if (code >= 300) return 'Redirect';
	if (code >= 200) return 'OK';
	return 'Unknown';
}

function buildExampleResponse(
	ex: ChaosRoundExample,
	label: string,
	request: Record<string, unknown>
): unknown {
	const st = statusText(ex.statusCode);
	const isJson = ex.headers?.['Content-Type']?.includes('json') ?? ex.body?.trimStart().startsWith('{') ?? false;
	return {
		name: `${ex.statusCode} ${st} — ${label}`,
		originalRequest: request,
		status: st,
		code: ex.statusCode,
		header: Object.entries(ex.headers ?? {}).map(([key, value]) => ({ key, value })),
		body: ex.body ?? '',
		_postman_previewlanguage: isJson ? 'json' : 'text'
	};
}

function buildChaosExamples(round: ChaosRound, request: Record<string, unknown>): unknown[] {
	const sources: ChaosRoundExample[] = round.examples?.length
		? round.examples
		: round.statusCode
			? [{ statusCode: round.statusCode, body: round.body, headers: round.headers, durationMs: round.durationMs }]
			: [];

	if (sources.length === 1) {
		return [buildExampleResponse(sources[0], 'chaos probe', request)];
	}
	return sources.map((ex, i) => buildExampleResponse(ex, `round ${i + 1} of ${sources.length}`, request));
}

export function collectionItems(gw: GatewaySummary, chaosResults?: Map<string, ChaosRound>): unknown[] {
	const details = gw.routeDetails;
	if (details && details.length > 0) {
		return details.map((detail) => {
			const method = detail.method ?? 'GET';
			const exportedMethod = method === 'ANY' || method === '$default' ? 'GET' : method.toUpperCase();
			const rawPath = method === '$default'
				? '{{invokeBase}}/{{defaultPath}}'
				: `{{invokeBase}}${postmanPath(detail.path ?? '/')}`;
			let description = `Generated from ${detail.routeKey}.`;
			if (method === 'ANY') description = `Generated from ${detail.routeKey}. Exported as GET placeholder — change method as needed.`;
			else if (method === '$default') description = 'Generated from $default route. Set {{defaultPath}} before sending.';
			if (detail.integrationTarget) {
				const { kind, name } = parseTarget(detail.integrationTarget);
				description += kind ? ` → ${kind} ${name}` : ` → ${name}`;
			}
			const request: Record<string, unknown> = {
				method: exportedMethod,
				header: buildItemHeaders(detail),
				url: rawPath,
				description
			};
			const body = buildItemBody(detail);
			if (body) request.body = body;
			const response: unknown[] = [];
			const round = chaosResults?.get(detail.routeKey);
			if (round) response.push(...buildChaosExamples(round, request));
			return { name: detail.routeKey, request, response };
		});
	}
	return (gw.routeKeys ?? []).map((routeKey) => {
		const { method, path } = routeKeyParts(routeKey);
		const exportedMethod = method === 'ANY' || method === '$default' ? 'GET' : method;
		const rawPath = method === '$default'
			? '{{invokeBase}}/{{defaultPath}}'
			: `{{invokeBase}}${postmanPath(path)}`;
		const request = {
			method: exportedMethod,
			header: exportedMethod === 'GET' || exportedMethod === 'HEAD'
				? []
				: [{ key: 'Content-Type', value: 'application/json', type: 'text' }],
			url: rawPath,
			description: method === 'ANY'
				? `Generated from ${routeKey}. Exported as GET placeholder.`
				: method === '$default'
					? 'Generated from $default route. Set {{defaultPath}} before sending.'
					: `Generated from ${routeKey}.`
		};
		const response: unknown[] = [];
		const round = chaosResults?.get(routeKey);
		if (round) response.push(...buildChaosExamples(round, request));
		return { name: routeKey, request, response };
	});
}

export function buildPostmanCollection(gw: GatewaySummary, chaosResults?: Map<string, ChaosRound>) {
	return {
		info: {
			name: `${gw.name} (Tarn API Gateway)`,
			description: `Generated from Tarn API Gateway ${gw.apiId}.`,
			schema: 'https://schema.getpostman.com/json/collection/v2.1.0/collection.json'
		},
		variable: collectionVariables(gw),
		item: collectionItems(gw, chaosResults)
	};
}

export function buildPostmanEnvironment(gw: GatewaySummary) {
	return {
		name: `${gw.name} (Tarn Local)`,
		values: collectionVariables(gw).map((v) => ({ key: v.key, value: v.value, enabled: true, type: v.type ?? 'text' })),
		_postman_variable_scope: 'environment',
		_postman_exported_at: new Date().toISOString(),
		_postman_exported_using: 'Tarn dashboard'
	};
}

/** Combined collection: each gateway is a folder, URLs are inlined (no shared variables). */
export function buildCombinedCollection(gateways: GatewaySummary[]) {
	const folders = gateways.map((gw) => {
		const invokeBase = normalizeInvokeUrl(gw.invokeUrl);
		const details = gw.routeDetails;
		let items: unknown[];

		if (details && details.length > 0) {
			items = details.map((detail) => {
				const method = detail.method ?? 'GET';
				const exportedMethod = method === 'ANY' || method === '$default' ? 'GET' : method.toUpperCase();
				const rawPath = method === '$default'
					? `${invokeBase}/`
					: `${invokeBase}${postmanPath(detail.path ?? '/')}`;
				let description = `Generated from ${detail.routeKey}.`;
				if (method === 'ANY') description += ' Exported as GET placeholder.';
				if (detail.integrationTarget) {
					const { kind, name } = parseTarget(detail.integrationTarget);
					description += kind ? ` → ${kind} ${name}` : ` → ${name}`;
				}
				const request: Record<string, unknown> = {
					method: exportedMethod,
					header: buildItemHeaders(detail),
					url: rawPath,
					description
				};
				const body = buildItemBody(detail);
				if (body) request.body = body;
				return { name: detail.routeKey, request, response: [] };
			});
		} else {
			items = (gw.routeKeys ?? []).map((routeKey) => {
				const { method, path } = routeKeyParts(routeKey);
				const exportedMethod = method === 'ANY' || method === '$default' ? 'GET' : method;
				return {
					name: routeKey,
					request: {
						method: exportedMethod,
						header: exportedMethod === 'GET' || exportedMethod === 'HEAD'
							? []
							: [{ key: 'Content-Type', value: 'application/json', type: 'text' }],
						url: `${invokeBase}${postmanPath(path)}`,
						description: `Generated from ${routeKey}.`
					},
					response: []
				};
			});
		}

		return {
			name: gw.name,
			description: `${gw.name} — ${gw.protocolType} (${gw.version})\n${invokeBase}`,
			item: items
		};
	});

	return {
		info: {
			name: 'Tarn — All API Gateways',
			description: `Combined collection for all ${gateways.length} API Gateway(s). Generated by Tarn dashboard.`,
			schema: 'https://schema.getpostman.com/json/collection/v2.1.0/collection.json'
		},
		item: folders
	};
}

export function downloadJSON(filename: string, payload: unknown) {
	const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
	const href = URL.createObjectURL(blob);
	const link = document.createElement('a');
	link.href = href;
	link.download = filename;
	document.body.appendChild(link);
	link.click();
	link.remove();
	URL.revokeObjectURL(href);
}
