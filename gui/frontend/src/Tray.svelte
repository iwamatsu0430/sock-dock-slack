<script lang="ts">
  import { App, errorText } from "./lib/api";
  import { live, connection } from "./lib/live.svelte";
  import { ago, elapsed, statusLabel, statusLevel } from "./lib/format";
  import { t } from "./lib/i18n/index.svelte";

  let busy = $state(false);
  let message = $state("");

  const conn = $derived(connection(live.snap));
  const status = $derived(live.snap?.status);
  const running = $derived(status?.running ?? []);
  const recent = $derived(live.turns.filter((x) => x.status !== "running").slice(0, 6));

  async function act(fn: () => Promise<unknown>, done: string) {
    busy = true;
    message = "";
    try {
      await fn();
      message = done;
    } catch (e) {
      message = errorText(e);
    } finally {
      busy = false;
    }
  }

  function link(url?: string) {
    if (url) App.OpenURL(url);
  }
</script>

<div class="tray">
  <header class="row">
    <span class="dot {conn.level}"></span>
    <div class="title">
      <h2>SockDock</h2>
      <p class="small muted">{conn.text}</p>
    </div>
    <span class="spacer"></span>
    {#if status?.identity?.team}<span class="badge">{status.identity.team}</span>{/if}
  </header>

  <section class="body">
    {#if live.snap && !live.snap.running}
      <div class="card stack">
        <p>{t("tray.notRunning")}</p>
        <p class="small muted">{t("tray.notRunningHelp")}</p>
        <div class="row">
          <button class="primary" disabled={busy} onclick={() => act(() => App.InstallService(), t("tray.serviceStarted"))}>{t("tray.startService")}</button>
          <button onclick={() => App.OpenWindow("setup")}>{t("tray.setup")}</button>
        </div>
      </div>
    {:else}
      <h3 class="section">{t("tray.running")}</h3>
      {#if running.length === 0}
        <p class="small muted empty">{t("tray.nothingRunning")}</p>
      {:else}
        <ul class="list">
          {#each running as r (r.turn_id)}
            <li class="row">
              <span class="dot ok pulse"></span>
              <span class="route">{r.route}</span>
              <span class="spacer"></span>
              <span class="small muted">{elapsed(r.started_at, live.now)}</span>
              {#if r.permalink}<button type="button" class="link small" onclick={() => link(r.permalink)}>{t("action.open")}</button>{/if}
            </li>
          {/each}
        </ul>
      {/if}

      <h3 class="section">{t("tray.recent")}</h3>
      {#if recent.length === 0}
        <p class="small muted empty">{t("tray.noRuns")}</p>
      {:else}
        <ul class="list">
          {#each recent as x (x.id)}
            <li class="row">
              <span class="badge {statusLevel(x.status)}">{statusLabel(x.status)}</span>
              <span class="route">{x.route}</span>
              <span class="spacer"></span>
              <span class="small muted">{ago(x.finished_at ?? x.started_at, live.now)}</span>
            </li>
          {/each}
        </ul>
      {/if}

      {#if (status?.routes ?? []).some((r) => r.paused)}
        <p class="small muted paused">{t("tray.paused", { list: status!.routes.filter((r) => r.paused).map((r) => r.name).join(", ") })}</p>
      {/if}
    {/if}
    {#if message}<p class="small muted msg">{message}</p>{/if}
  </section>

  <footer class="row">
    <button onclick={() => App.OpenWindow("overview")}>{t("tray.openMain")}</button>
    <button disabled={busy || !live.snap?.running} onclick={() => act(() => App.Reload(), t("tray.reloaded"))}>{t("action.reload")}</button>
    <span class="spacer"></span>
    <button class="small" title={t("tray.quitTitle")} onclick={() => App.Quit()}>{t("action.quit")}</button>
  </footer>
</div>

<style>
  .tray {
    height: 100vh;
    display: flex;
    flex-direction: column;
    background: color-mix(in srgb, var(--bg) 92%, transparent);
  }
  header {
    padding: 14px 16px 10px;
    border-bottom: 1px solid var(--border);
  }
  .title p { margin-top: -2px; }
  .body {
    flex: 1;
    overflow-y: auto;
    padding: 10px 16px;
  }
  .section {
    margin: 10px 0 4px;
    color: var(--muted);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .list li {
    padding: 5px 0;
    border-bottom: 1px solid var(--border);
  }
  .list li:last-child { border-bottom: none; }
  .route { font-weight: 500; }
  .empty { padding: 4px 0; }
  .paused, .msg { margin-top: 10px; }
  footer {
    padding: 10px 16px;
    border-top: 1px solid var(--border);
  }
  .pulse { animation: pulse 1.4s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: 0.35; } }
</style>
