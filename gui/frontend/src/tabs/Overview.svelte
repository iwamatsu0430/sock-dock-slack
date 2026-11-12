<script lang="ts">
  import { App, errorText } from "../lib/api";
  import { live, connection } from "../lib/live.svelte";
  import { ago, clock, duration, elapsed, statusLevel } from "../lib/format";

  let { onnavigate }: { onnavigate: (tab: string) => void } = $props();

  let error = $state("");
  const conn = $derived(connection(live.snap));
  const status = $derived(live.snap?.status);

  async function togglePause(name: string, paused: boolean) {
    error = "";
    try {
      await App.SetPaused(name, !paused);
    } catch (e) {
      error = errorText(e);
    }
  }
</script>

<div class="stack">
  <div class="row head">
    <h1>Overview</h1>
  </div>

  {#if live.snap && !live.snap.running}
    <div class="card stack">
      <div class="row"><span class="dot bad"></span><h2>The gateway is not running</h2></div>
      <p class="muted">
        sockdock runs as a background service (a launchd agent) so it keeps working when this window is closed. Set up your Slack tokens and config,
        then start the service.
      </p>
      <div class="row">
        <button class="primary" onclick={() => onnavigate("setup")}>Go to Setup</button>
      </div>
    </div>
  {:else if status}
    <div class="grid3">
      <div class="card">
        <p class="small muted">Slack</p>
        <div class="row"><span class="dot {conn.level}"></span><strong>{status.connected ? "Connected" : "Disconnected"}</strong></div>
        <p class="small muted">{status.identity?.team ?? ""}</p>
      </div>
      <div class="card">
        <p class="small muted">Gateway</p>
        <strong>v{status.version}</strong>
        <p class="small muted">up {elapsed(status.started_at, live.now)} · pid {status.pid}</p>
      </div>
      <div class="card">
        <p class="small muted">Last event</p>
        <strong>{ago(status.last_event_at, live.now)}</strong>
        <p class="small muted">{(status.running ?? []).length} running · {status.pending} queued</p>
      </div>
    </div>
    {#if status.last_error && !status.connected}
      <div class="error-box">{status.last_error}</div>
    {/if}

    <div class="card">
      <div class="row"><h2>Routes</h2><span class="spacer"></span><button class="small" onclick={() => onnavigate("routes")}>Manage…</button></div>
      {#if (status.routes ?? []).length === 0}
        <p class="muted small">No routes yet. Add one in Routes.</p>
      {:else}
        <table>
          <thead><tr><th>Name</th><th>Type</th><th>Channel</th><th>Match</th><th></th></tr></thead>
          <tbody>
            {#each status.routes as r (r.name)}
              <tr>
                <td><strong>{r.name}</strong></td>
                <td>{r.type}</td>
                <td class="mono">{r.channel}</td>
                <td class="mono match">{r.match}</td>
                <td class="right">
                  <button class="small" onclick={() => togglePause(r.name, r.paused)}>{r.paused ? "Resume" : "Pause"}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if error}<div class="error-box">{error}</div>{/if}
    </div>

    <div class="card">
      <h2>Recent runs</h2>
      {#if live.turns.length === 0}
        <p class="muted small">No handler runs yet.</p>
      {:else}
        <table>
          <thead><tr><th>Started</th><th>Route</th><th>Status</th><th>Duration</th><th>Detail</th></tr></thead>
          <tbody>
            {#each live.turns as t (t.id)}
              <tr>
                <td class="nowrap">{clock(t.started_at)}</td>
                <td>{t.route}</td>
                <td><span class="badge {statusLevel(t.status)}">{t.status}</span></td>
                <td>{t.status === "running" ? elapsed(t.started_at, live.now) : duration(t.started_at, t.finished_at)}</td>
                <td class="small muted detail">{t.error ?? ""}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {/if}
</div>

<style>
  .head { min-height: 32px; }
  .grid3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
  .card h2 { margin-bottom: 8px; }
  .match { max-width: 260px; word-break: break-all; }
  .right { text-align: right; }
  .nowrap { white-space: nowrap; }
  .detail { max-width: 300px; word-break: break-word; user-select: text; }
</style>
