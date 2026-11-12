<script lang="ts">
  import { App, errorText } from "../lib/api";
  import { live, connection, gatewayText } from "../lib/live.svelte";
  import { ago, clock, duration, elapsed, statusLabel, statusLevel } from "../lib/format";
  import { t } from "../lib/i18n/index.svelte";
  import Setup from "./Setup.svelte";

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
    <h1>{t("tab.overview")}</h1>
  </div>

  {#if live.snap && !live.snap.running}
    <!-- Not running yet: setup comes first. -->
    <div class="card row">
      <span class="dot {live.snap.gateway?.state === 'setup' ? 'warn' : 'bad'}"></span>
      <span>{gatewayText(live.snap.gateway)}</span>
      <span class="spacer"></span>
      {#if live.snap.gateway?.state === "stopped" || live.snap.gateway?.state === "error"}
        <button class="primary small" onclick={() => App.StartGateway()}>{t("gw.start")}</button>
      {/if}
    </div>
    <h2 class="setup-title">{t("overview.setupTitle")}</h2>
    <Setup {onnavigate} />
  {:else if status}
    <div class="grid3">
      <div class="card">
        <p class="small muted">{t("overview.slack")}</p>
        <div class="row"><span class="dot {conn.level}"></span><strong>{status.connected ? t("overview.connected") : t("overview.disconnected")}</strong></div>
        <p class="small muted">{status.identity?.team ?? ""}</p>
      </div>
      <div class="card">
        <p class="small muted">{t("overview.gateway")}</p>
        <strong>v{status.version}</strong>
        <p class="small muted">{t("overview.uptime", { elapsed: elapsed(status.started_at, live.now), pid: status.pid })}</p>
      </div>
      <div class="card">
        <p class="small muted">{t("overview.lastEvent")}</p>
        <strong>{ago(status.last_event_at, live.now)}</strong>
        <p class="small muted">{t("overview.load", { running: (status.running ?? []).length, queued: status.pending })}</p>
      </div>
    </div>
    {#if status.last_error && !status.connected}
      <div class="error-box">{status.last_error}</div>
    {/if}

    <div class="card">
      <div class="row"><h2>{t("overview.routes")}</h2><span class="spacer"></span><button class="small" onclick={() => onnavigate("routes")}>{t("overview.manage")}</button></div>
      {#if (status.routes ?? []).length === 0}
        <p class="muted small">{t("overview.noRoutes")}</p>
      {:else}
        <table>
          <thead><tr><th>{t("col.name")}</th><th>{t("col.type")}</th><th>{t("col.channel")}</th><th>{t("col.match")}</th><th></th></tr></thead>
          <tbody>
            {#each status.routes as r (r.name)}
              <tr>
                <td><strong>{r.name}</strong></td>
                <td>{r.type}</td>
                <td class="mono">{r.channel}</td>
                <td class="mono match">{r.match}</td>
                <td class="right">
                  <button class="small" onclick={() => togglePause(r.name, r.paused)}>{r.paused ? t("action.resume") : t("action.pause")}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if error}<div class="error-box">{error}</div>{/if}
    </div>

    <div class="card">
      <h2>{t("overview.recentRuns")}</h2>
      {#if live.turns.length === 0}
        <p class="muted small">{t("overview.noRuns")}</p>
      {:else}
        <table>
          <thead><tr><th>{t("col.started")}</th><th>{t("col.route")}</th><th>{t("col.status")}</th><th>{t("col.duration")}</th><th>{t("col.detail")}</th></tr></thead>
          <tbody>
            {#each live.turns as x (x.id)}
              <tr>
                <td class="nowrap">{clock(x.started_at)}</td>
                <td>{x.route}</td>
                <td><span class="badge {statusLevel(x.status)}">{statusLabel(x.status)}</span></td>
                <td>{x.status === "running" ? elapsed(x.started_at, live.now) : duration(x.started_at, x.finished_at)}</td>
                <td class="small muted detail">{x.error ?? ""}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <!-- Running: keep setup at hand for changing tokens or the login item. -->
    <details class="card setup-details">
      <summary>{t("overview.setupTitle")}</summary>
      <div class="setup-body"><Setup {onnavigate} /></div>
    </details>
  {/if}
</div>

<style>
  .head { min-height: 32px; }
  .setup-title { margin-top: 4px; }
  .setup-details summary { cursor: pointer; font-weight: 600; }
  .setup-body { margin-top: 12px; }
  .grid3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
  .card h2 { margin-bottom: 8px; }
  .match { max-width: 260px; word-break: break-all; }
  .right { text-align: right; }
  .nowrap { white-space: nowrap; }
  .detail { max-width: 300px; word-break: break-word; user-select: text; }
</style>
