<script lang="ts">
  import { onDestroy } from "svelte";
  import { PlayIcon, TrashIcon, TerminalIcon, WarningCircleIcon } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import { execRedisCommand } from "#lib/api";
  import { notifyRedisMutation } from "#lib/redis";
  import type { RedisAuthOptions } from "#lib/types";

  let {
    targetId,
    auth,
    onMutated = () => {},
  }: {
    targetId: string;
    auth: RedisAuthOptions;
    onMutated?: () => void;
  } = $props();

  interface HistoryEntry {
    id: number;
    command: string;
    output: string;
    error?: string;
    durationMs?: number;
  }

  const PRESETS = ["PING", "DBSIZE", "INFO keyspace", "INFO memory", "CLIENT LIST", "SLOWLOG GET 5"];
  const WRITE_VERBS = new Set(["SET", "DEL", "UNLINK", "EXPIRE", "PEXPIRE", "PERSIST", "HSET", "HDEL", "LPUSH", "RPUSH", "LPOP", "RPOP", "SADD", "SREM", "ZADD", "ZREM", "XADD", "XDEL", "FLUSHDB", "FLUSHALL", "INCR", "DECR", "MSET", "RENAME"]);

  let commandInput = $state("");
  let running = $state(false);
  let entries = $state<HistoryEntry[]>([]);
  let cmdHistory = $state<string[]>([]);
  let historyIdx = $state(-1);
  let nextId = 1;
  let controller: AbortController | null = null;

  onDestroy(() => {
    controller?.abort();
  });

  async function runCommand(raw?: string) {
    const cmd = (raw ?? commandInput).trim();
    if (!cmd || running) return;
    controller?.abort();
    const ac = new AbortController();
    controller = ac;
    running = true;
    commandInput = "";
    cmdHistory = [cmd, ...cmdHistory.filter((c) => c !== cmd)].slice(0, 40);
    historyIdx = -1;

    try {
      const res = await execRedisCommand(targetId, { ...auth, command: cmd }, ac.signal);
      if (ac.signal.aborted) return;
      entries = [
        { id: nextId++, command: cmd, output: res.formatted, durationMs: res.durationMs },
        ...entries,
      ].slice(0, 50);

      const verb = cmd.split(/\s+/)[0]?.toUpperCase() ?? "";
      if (WRITE_VERBS.has(verb)) {
        notifyRedisMutation(targetId, auth.db ?? 0);
        onMutated();
      }
    } catch (e) {
      if (ac.signal.aborted) return;
      entries = [
        { id: nextId++, command: cmd, output: "", error: e instanceof Error ? e.message : "Command failed" },
        ...entries,
      ].slice(0, 50);
    } finally {
      if (controller === ac) {
        running = false;
        controller = null;
      }
    }
  }

  function onInputKeydown(e: KeyboardEvent) {
    if (e.key === "ArrowUp" && cmdHistory.length > 0) {
      e.preventDefault();
      historyIdx = Math.min(cmdHistory.length - 1, historyIdx + 1);
      commandInput = cmdHistory[historyIdx];
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      if (historyIdx <= 0) {
        historyIdx = -1;
        commandInput = "";
      } else {
        historyIdx -= 1;
        commandInput = cmdHistory[historyIdx];
      }
    }
  }
</script>

<RcPanel
  title="Command Console"
  description="Run single Redis commands against db{auth.db ?? 0}. Blocking/stream-hijacking commands (MONITOR, SUBSCRIBE, BLPOP, SHUTDOWN) are blocked."
  index={0}
>
  {#snippet actions()}
    {#if entries.length > 0}
      <RcButton small variant="ghost" onclick={() => (entries = [])}>
        <TrashIcon size={12} /> Clear
      </RcButton>
    {/if}
  {/snippet}

  <div class="presets">
    {#each PRESETS as preset (preset)}
      <button type="button" class="preset mono" disabled={running} onclick={() => runCommand(preset)}>
        {preset}
      </button>
    {/each}
  </div>

  <form class="cmd-form" onsubmit={(e) => { e.preventDefault(); void runCommand(); }}>
    <span class="prompt mono"><TerminalIcon size={12} /> db{auth.db ?? 0}&gt;</span>
    <input
      class="mono"
      bind:value={commandInput}
      onkeydown={onInputKeydown}
      placeholder="GET user:1  |  HGETALL session:abc  |  TTL mykey"
      aria-label="Redis command"
      disabled={running}
    />
    <RcButton small type="submit" disabled={running || !commandInput.trim()}>
      <PlayIcon size={11} weight="fill" /> {running ? "Running..." : "Run"}
    </RcButton>
  </form>

  {#if entries.length === 0}
    <p class="empty">Run a command above or pick a quick preset.</p>
  {:else}
    <div class="history">
      {#each entries as entry (entry.id)}
        <div class="entry" class:has-error={!!entry.error}>
          <div class="entry-head">
            <code class="entry-cmd">&gt; {entry.command}</code>
            {#if entry.durationMs !== undefined}
              <span class="entry-dur mono">{entry.durationMs}ms</span>
            {/if}
          </div>
          {#if entry.error}
            <div class="entry-err">
              <WarningCircleIcon size={13} />
              <span>{entry.error}</span>
            </div>
          {:else}
            <pre class="entry-out">{entry.output}</pre>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</RcPanel>

<style>
  .presets { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 12px; }
  .preset {
    padding: 3px 8px; border-radius: 6px; border: 1px solid var(--border-subtle);
    background: var(--bg-element); color: var(--text-secondary); font-size: 11px; cursor: pointer;
  }
  .preset:hover { color: var(--text-primary); border-color: var(--border-default); }
  .cmd-form {
    display: flex; align-items: center; gap: 8px; padding: 5px 6px 5px 10px;
    border-radius: 8px; border: 1px solid var(--border-subtle); background: var(--bg-element);
    margin-bottom: 14px;
  }
  .cmd-form:focus-within { border-color: var(--border-focus); }
  .prompt { display: inline-flex; align-items: center; gap: 5px; font-size: 11.5px; color: var(--topology-external-redis, var(--accent-amber)); flex-shrink: 0; }
  .cmd-form input {
    flex: 1; min-width: 0; border: 0; outline: none; background: transparent;
    color: var(--text-primary); font-size: 12px;
  }
  .empty { padding: 18px 8px; font-size: 12px; color: var(--text-tertiary); text-align: center; }
  .history { display: flex; flex-direction: column; gap: 8px; }
  .entry {
    border: 1px solid var(--border-subtle); border-radius: 8px;
    background: var(--bg-element); overflow: hidden;
  }
  .entry.has-error { border-color: color-mix(in srgb, var(--accent-red) 40%, transparent); }
  .entry-head {
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
    padding: 7px 10px; border-bottom: 1px solid var(--border-subtle);
  }
  .entry-cmd { font-size: 11.5px; color: var(--text-primary); }
  .entry-dur { font-size: 10.5px; color: var(--text-tertiary); }
  .entry-out {
    margin: 0; padding: 9px 10px; font: 11.5px/1.5 var(--font-mono, ui-monospace, monospace);
    color: var(--text-secondary); white-space: pre-wrap; word-break: break-word; max-height: 280px; overflow-y: auto;
  }
  .entry-err {
    display: flex; align-items: center; gap: 7px; padding: 9px 10px;
    color: var(--accent-red); font-size: 12px;
  }
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }
</style>
