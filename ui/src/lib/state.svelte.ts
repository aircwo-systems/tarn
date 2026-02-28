import { fetchOverview } from '$lib/api';
import type { OverviewResponse } from '$lib/types';

let data = $state<OverviewResponse | null>(null);
let loading = $state(true);
let error = $state('');
let lastRefresh = $state('');

let pollHandle: ReturnType<typeof setInterval> | null = null;
let inFlight = false;

const SETTINGS_COOKIE = 'openstack-ui-settings';
const DEFAULT_POLLING_INTERVAL_SECONDS = 5;
const MIN_POLLING_INTERVAL_SECONDS = 1;
const MAX_POLLING_INTERVAL_SECONDS = 120;
const DEFAULT_PERSISTENCE_ENABLED = false;

export type ThemeMode = 'system' | 'light' | 'dark';

let pollingIntervalSeconds = $state(DEFAULT_POLLING_INTERVAL_SECONDS);
let themeMode = $state<ThemeMode>('system');
let resolvedTheme = $state<'light' | 'dark'>('dark');
let persistenceEnabled = $state(DEFAULT_PERSISTENCE_ENABLED);
let dashboardTagFilter = $state('');
let settingsInitialized = false;

let systemThemeMediaQuery: MediaQueryList | null = null;
let systemThemeListener: ((event: MediaQueryListEvent) => void) | null = null;

export function getDashboard() {
	return {
		get data() { return data; },
		get loading() { return loading; },
		get error() { return error; },
		get lastRefresh() { return lastRefresh; }
	};
}

export function getUISettings() {
	return {
		get pollingIntervalSeconds() { return pollingIntervalSeconds; },
		get themeMode() { return themeMode; },
		get resolvedTheme() { return resolvedTheme; },
		get persistenceEnabled() { return persistenceEnabled; }
	};
}

export function getDashboardFilters() {
	return {
		get tagFilter() { return dashboardTagFilter; }
	};
}

export async function refresh() {
	if (inFlight) return;
	inFlight = true;
	if (!loading) error = '';

	try {
		data = await fetchOverview();
		lastRefresh = new Date().toLocaleTimeString();
		error = '';
	} catch (err) {
		error = err instanceof Error ? err.message : 'Failed to load dashboard data';
	} finally {
		loading = false;
		inFlight = false;
	}
}

export function startPolling() {
	initUISettings();
	refresh();
	schedulePolling();
}

export function stopPolling() {
	if (pollHandle) {
		clearInterval(pollHandle);
		pollHandle = null;
	}
}

export function initUISettings() {
	if (settingsInitialized) return;
	settingsInitialized = true;

	const settings = readSettingsFromCookie();
	pollingIntervalSeconds = normalizePollingInterval(settings.pollingIntervalSeconds);
	themeMode = normalizeThemeMode(settings.themeMode);
	persistenceEnabled = normalizePersistenceEnabled(settings.persistenceEnabled);
	applyTheme(themeMode);
}

export function setPollingIntervalSeconds(next: number) {
	const normalized = normalizePollingInterval(next);
	if (normalized === pollingIntervalSeconds) return;

	pollingIntervalSeconds = normalized;
	persistSettingsToCookie();
	restartPollingIfActive();
}

export function setThemeMode(next: ThemeMode) {
	const normalized = normalizeThemeMode(next);
	if (normalized === themeMode) return;

	themeMode = normalized;
	applyTheme(themeMode);
	persistSettingsToCookie();
}

export function setPersistenceEnabled(next: boolean) {
	const normalized = normalizePersistenceEnabled(next);
	if (normalized === persistenceEnabled) return;

	persistenceEnabled = normalized;
	persistSettingsToCookie();
}

export function setDashboardTagFilter(next: string) {
	dashboardTagFilter = next.trim();
}

export function matchesTagFilter(tags: Record<string, string> | undefined, query: string): boolean {
	const normalized = query.trim().toLowerCase();
	if (!normalized) return true;
	if (!tags || Object.keys(tags).length === 0) return false;

	const pairSeparator = normalized.includes(':') ? ':' : normalized.includes('=') ? '=' : '';
	if (pairSeparator) {
		const [rawKey, ...rest] = normalized.split(pairSeparator);
		const keyQuery = rawKey.trim();
		const valueQuery = rest.join(pairSeparator).trim();

		for (const [key, value] of Object.entries(tags)) {
			const keyLower = key.toLowerCase();
			const valueLower = value.toLowerCase();
			if (keyQuery && !keyLower.includes(keyQuery)) {
				continue;
			}
			if (valueQuery && !valueLower.includes(valueQuery)) {
				continue;
			}
			return true;
		}
		return false;
	}

	for (const [key, value] of Object.entries(tags)) {
		const keyLower = key.toLowerCase();
		const valueLower = value.toLowerCase();
		if (
			keyLower.includes(normalized) ||
			valueLower.includes(normalized) ||
			`${keyLower}:${valueLower}`.includes(normalized)
		) {
			return true;
		}
	}
	return false;
}

function schedulePolling() {
	stopPolling();
	pollHandle = setInterval(() => {
		if (!document.hidden) {
			refresh();
		}
	}, pollingIntervalSeconds * 1000);
}

function restartPollingIfActive() {
	if (!pollHandle) return;
	schedulePolling();
}

function applyTheme(mode: ThemeMode) {
	if (typeof window === 'undefined' || typeof document === 'undefined') return;

	const root = document.documentElement;
	const resolved = resolveThemeMode(mode);

	resolvedTheme = resolved;
	root.classList.toggle('dark', resolved === 'dark');
	root.classList.toggle('light', resolved === 'light');

	if (mode === 'system') {
		ensureSystemThemeListener();
	} else {
		detachSystemThemeListener();
	}
}

function resolveThemeMode(mode: ThemeMode): 'light' | 'dark' {
	if (mode === 'light' || mode === 'dark') return mode;
	if (typeof window === 'undefined') return 'dark';
	return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function ensureSystemThemeListener() {
	if (typeof window === 'undefined') return;
	if (!systemThemeMediaQuery) {
		systemThemeMediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
	}
	if (systemThemeListener) return;

	systemThemeListener = () => {
		if (themeMode !== 'system') return;
		applyTheme('system');
	};
	systemThemeMediaQuery.addEventListener('change', systemThemeListener);
}

function detachSystemThemeListener() {
	if (!systemThemeMediaQuery || !systemThemeListener) return;
	systemThemeMediaQuery.removeEventListener('change', systemThemeListener);
	systemThemeListener = null;
}

function persistSettingsToCookie() {
	if (typeof document === 'undefined') return;

	const payload = encodeURIComponent(JSON.stringify({
		pollingIntervalSeconds,
		themeMode,
		persistenceEnabled
	}));
	document.cookie = `${SETTINGS_COOKIE}=${payload}; Path=/; Max-Age=31536000; SameSite=Lax`;
}

function readSettingsFromCookie(): { pollingIntervalSeconds?: number; themeMode?: ThemeMode; persistenceEnabled?: boolean } {
	if (typeof document === 'undefined') return {};

	const raw = document.cookie
		.split('; ')
		.find((entry) => entry.startsWith(`${SETTINGS_COOKIE}=`));

	if (!raw) return {};

	const encoded = raw.slice(SETTINGS_COOKIE.length + 1);
	try {
		const parsed = JSON.parse(decodeURIComponent(encoded)) as {
			pollingIntervalSeconds?: number;
			themeMode?: ThemeMode;
			persistenceEnabled?: boolean;
		};
		return parsed ?? {};
	} catch {
		return {};
	}
}

function normalizePollingInterval(value: unknown): number {
	const numeric = typeof value === 'number' ? value : Number(value);
	if (!Number.isFinite(numeric)) return DEFAULT_POLLING_INTERVAL_SECONDS;
	const rounded = Math.round(numeric);
	return Math.min(MAX_POLLING_INTERVAL_SECONDS, Math.max(MIN_POLLING_INTERVAL_SECONDS, rounded));
}

function normalizeThemeMode(value: unknown): ThemeMode {
	if (value === 'light' || value === 'dark' || value === 'system') {
		return value;
	}
	return 'system';
}

function normalizePersistenceEnabled(value: unknown): boolean {
	return value === true;
}
