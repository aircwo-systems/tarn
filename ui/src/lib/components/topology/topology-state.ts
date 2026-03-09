import { derived, writable } from 'svelte/store';

// --- 1. Raw Data Stores (The "Source of Truth") ---
export const gateways = writable<any[]>([]);
export const functions = writable<any[]>([]);
export const queues = writable<any[]>([]);
export const secrets = writable<any[]>([]);
export const buckets = writable<any[]>([]);
export const infra = writable<any[]>([]);
export const dashboard = writable({ loading: false });

// --- 2. Canvas Constants & Geometry ---
export const CW = 1000; // Connection View Width
export const CH = 800;  // Connection View Height
export const W = 820;   // Component View Width
export const H = 620;   // Component View Height

export const connNodeHalfWidth = 48;
export const cacheNodeHalfWidth = 52;
export const infraNodeHalfWidth = 50;

export const serviceX = {
    apigw: 120,
    lambda: 280,
    sqs: 440,
    secrets: 580,
    s3: 720,
    infra: 880
};

export const laneY = 140;

// --- 3. Visual Helpers (Colors & States) ---
export const ledColorMap: Record<string, string> = {
    green: 'var(--color-accent)',
    red: 'var(--color-red)',
    amber: 'var(--color-amber)',
    blue: 'var(--color-blue)',
    gray: 'var(--color-text-faint)'
};

export const stateColor = (state: string): string => {
    switch (state?.toLowerCase()) {
        case 'active': return 'green';
        case 'pending': return 'amber';
        case 'error': case 'failed': return 'red';
        default: return 'gray';
    }
};

// --- 4. Coordinate Generators (The "Math" Layer) ---

/** Calculates S-Curve paths between nodes with lane-offsetting to prevent overlaps */
export const laneAwarePath = (
    from: {x: number, y: number}, 
    to: {x: number, y: number}, 
    lane: number, 
    totalLanes: number,
    fromOffset = 0,
    toOffset = 0
) => {
    const startX = from.x + fromOffset;
    const endX = to.x - toOffset;
    const midX = (startX + endX) / 2;
    // Offset the Y mid-point based on which lane this connection belongs to
    const laneOffset = (lane - (totalLanes - 1) / 2) * 12; 
    
    return `M ${startX} ${from.y} 
            C ${midX} ${from.y + laneOffset}, 
              ${midX} ${to.y + laneOffset}, 
              ${endX} ${to.y}`;
};

// --- 5. Derived UI State (The "Renderer" Input) ---

/** Transforms raw gateway data into SVG-ready node objects */
export const connGateways = derived(gateways, ($gw) => {
    return $gw.map((node, i) => ({
        id: node.apiId,
        x: 100, // Fixed column for gateways
        y: 150 + (i * 60),
        label: node.name.length > 8 ? node.name.slice(0, 8) + '..' : node.name,
        sub: 'API Gateway'
    }));
});

/** Aggregate check if any data exists to show the "No Data" state */
export const hasData = derived(
    [gateways, functions, queues, infra],
    ([$g, $f, $q, $i]) => $g.length > 0 || $f.length > 0 || $q.length > 0 || $i.length > 0
);

export const connFunctions = derived(functions, ($fns) => {
    return $fns.map((fn, i) => ({
        id: fn.name,
        x: 550, // Standard Function Column
        y: 150 + (i * 60),
        label: fn.name.length > 8 ? fn.name.slice(0, 8) + '..' : fn.name,
        sub: fn.runtime
    }));
});

export const connQueues = derived(queues, ($q) => {
    return $q.map((q, i) => ({
        id: q.name,
        x: 300, // Standard Queue Column
        y: 150 + (i * 80),
        label: q.name,
        sub: 'SQS'
    }));
});

export const apigwToFnEdges = derived([connGateways, connFunctions], ([$gws, $fns]) => {
    const edges: any[] = [];
    $gws.forEach((gw, i) => {
        // Logic: Find function matching gateway route (simplified for example)
        if ($fns[i]) {
            edges.push({
                from: { x: gw.x + 48, y: gw.y },
                to: { x: $fns[i].x - 48, y: $fns[i].y },
                lane: i,
                laneCount: $gws.length,
                active: true
            });
        }
    });
    return edges;
});

export const connInfraNodes = derived(infra, ($infra) => {
    const laneX = 200; // Starting X of the ladder
    const laneYStart = 550; // Vertical position of the ladder
    
    return $infra.map((node, i) => ({
        id: node.id || i.toString(),
        x: laneX + 60 + (i * 120), // Spread nodes horizontally inside the ladder
        y: laneYStart + 50,
        label: node.name,
        status: node.status // 'connected' or 'disconnected'
    }));
});

/** Calculates the bounding box for the Infrastructure Ladder background */
export const connInfraLane = derived(connInfraNodes, ($nodes) => {
    if ($nodes.length === 0) return null;
    return {
        x: 180,
        y: 530,
        width: Math.max(600, $nodes.length * 130),
        height: 80
    };
});

export const queueToFnEdges = derived([connQueues, connFunctions], ([$qs, $fns]) => {
    const edges: any[] = [];
    $qs.forEach((q, i) => {
        // Simple 1:1 mapping for demo; replace with your actual trigger logic
        if ($fns[i]) {
            edges.push({
                from: { x: q.x + 48, y: q.y },
                to: { x: $fns[i].x - 48, y: $fns[i].y },
                lane: i,
                laneCount: $qs.length
            });
        }
    });
    return edges;
});