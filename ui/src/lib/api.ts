import type {
  OverviewResponse,
  QueueMessagesResponse,
  QueueMessageSummary,
  SecretValueResult,
  LogGroupSummary,
  LogEventsResponse
} from '$lib/types';

const configuredBase = normalizeBase(
  typeof import.meta !== 'undefined' && import.meta.env?.VITE_OPENSTACK_API_BASE
    ? String(import.meta.env.VITE_OPENSTACK_API_BASE)
    : ''
);

function endpoint(path: string): string {
  return `${configuredBase}${path}`;
}

export async function fetchOverview(signal?: AbortSignal): Promise<OverviewResponse> {
  const overviewPath = '/_openstack/admin/overview';
  const healthPath = '/_openstack/health';
  const overviewURL = endpoint(overviewPath);
  const response = await fetch(overviewURL, {
    method: 'GET',
    headers: {
      Accept: 'application/json'
    },
    signal
  });

  if (response.status === 404) {
    const healthURL = endpoint(healthPath);
    const health = await fetch(healthURL, {
      method: 'GET',
      headers: {
        Accept: 'application/json'
      },
      signal
    }).catch(() => null);

    if (health?.ok) {
      throw new Error(
        'Connected to OpenStack, but this instance does not expose /_openstack/admin/overview yet. Rebuild and restart OpenStack from the latest code.'
      );
    }
  }

  if (!response.ok) {
    let message = `HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body?.error) {
        message = body.error;
      }
    } catch {
      // Ignore non-JSON error bodies.
    }
    throw new Error(message);
  }

  const overview = (await response.json()) as OverviewResponse;
  await hydrateQueueMessages(overview, signal);
  return overview;
}

export async function fetchQueueMessages(
  queueName: string,
  signal?: AbortSignal
): Promise<QueueMessageSummary[]> {
  const messagesPath = `/_openstack/admin/queues/${encodeURIComponent(queueName)}/messages?limit=20`;
  const response = await fetch(endpoint(messagesPath), {
    method: 'GET',
    headers: {
      Accept: 'application/json'
    },
    signal
  });

  if (!response.ok) {
    throw new Error(`Failed to load queue messages: HTTP ${response.status}`);
  }

  const payload = (await response.json()) as QueueMessagesResponse;
  if (!Array.isArray(payload?.messages)) {
    return [];
  }
  return payload.messages;
}

export async function fetchSecretValue(
  secretName: string,
  signal?: AbortSignal
): Promise<SecretValueResult> {
  const secretPath = `/_openstack/admin/secrets/${encodeURIComponent(secretName)}/value`;
  const response = await fetch(endpoint(secretPath), {
    method: 'GET',
    headers: {
      Accept: 'application/json'
    },
    signal
  });

  if (!response.ok) {
    let message = `Failed to load secret value: HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body?.error) {
        message = body.error;
      }
    } catch {
      // Ignore non-JSON error bodies.
    }
    throw new Error(message);
  }

  const payload = (await response.json()) as SecretValueResult;
  return {
    name: payload.name ?? secretName,
    value: payload.value ?? '',
    valueType: payload.valueType ?? 'string'
  };
}

export async function fetchLogGroups(signal?: AbortSignal): Promise<LogGroupSummary[]> {
  const response = await fetch(endpoint('/_openstack/admin/logs/groups'), {
    method: 'GET',
    headers: { Accept: 'application/json' },
    signal
  });
  if (!response.ok) {
    throw new Error(`Failed to load log groups: HTTP ${response.status}`);
  }
  const data = await response.json();
  return Array.isArray(data) ? data : [];
}

export interface FetchLogEventsParams {
  limit?: number;
  offset?: number; // Deprecated
  level?: string;
  pattern?: string;
  stream?: string;
  cursor?: string;
}

export async function fetchLogEvents(
  groupName: string,
  params: FetchLogEventsParams = {},
  signal?: AbortSignal
): Promise<LogEventsResponse> {
  const query = new URLSearchParams();
  if (params.limit) query.set('limit', String(params.limit));
  if (params.offset) query.set('offset', String(params.offset)); // Deprecated
  if (params.level) query.set('level', params.level);
  if (params.pattern) query.set('pattern', params.pattern);
  if (params.stream) query.set('stream', params.stream);
  if (params.cursor) query.set('cursor', params.cursor);

  const qs = query.toString();
  const url = endpoint(`/_openstack/admin/logs/events/${encodeURIComponent(groupName)}${qs ? '?' + qs : ''}`);
  const response = await fetch(url, {
    method: 'GET',
    headers: { Accept: 'application/json' },
    signal
  });
  if (!response.ok) {
    throw new Error(`Failed to load log events: HTTP ${response.status}`);
  }
  return (await response.json()) as LogEventsResponse;
}

async function hydrateQueueMessages(overview: OverviewResponse, signal?: AbortSignal): Promise<void> {
  const queuesToHydrate = overview.queues.filter((queue) => {
    const totalMessages = queue.approxVisible + queue.approxInFlight + queue.approxDelayed;
    const hasRecentMessages = Array.isArray(queue.recentMessages) && queue.recentMessages.length > 0;
    return totalMessages > 0 && !hasRecentMessages;
  });

  await Promise.all(
    queuesToHydrate.map(async (queue) => {
      try {
        queue.recentMessages = await fetchQueueMessages(queue.name, signal);
      } catch {
        // Keep overview data even if queue detail fetch fails.
      }
    })
  );
}

function normalizeBase(raw: string): string {
  const input = raw.trim();
  if (!input) return '';

  try {
    const url = new URL(input);
    if (url.hostname === '0.0.0.0' || url.hostname === '::' || url.hostname === '[::]') {
      url.hostname = '127.0.0.1';
    }
    return url.toString().replace(/\/$/, '');
  } catch {
    return input.replace(/\/$/, '');
  }
}
