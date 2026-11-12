<script lang="ts">
  import { App, errorText } from "./lib/api";
  import { live, connection } from "./lib/live.svelte";
  import { ago, elapsed, statusLevel } from "./lib/format";

  let busy = $state(false);
  let message = $state("");

  const conn = $derived(connection(live.snap));
  const status = $derived(live.snap?.status);
  const running = $derived(status?.running ?? []);
  const recent = $derived(live.turns.filter((t) => t.status !== "running").slice(0, 6));

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
        <p>The gateway is not running.</p>
        <p class="small muted">It runs as a background service, separately from this menu.</p>
        <div class="row">
          <button class="primary" disabled={busy} onclick={() => act(() => App.InstallService(), "Service started")}>Start service</button>
          <button onclick={() => App.OpenWindow("setup")}>Setup…</button>
        </div>
      </div>
    {:else}
      <h3 class="section">Running</h3>
      {#if running.length === 0}
        <p class="small muted empty">Nothing is running.</p>
      {:else}
        <ul class="list">
          {#each running as r (r.turn_id)}
            <li class="row">
              <span class="dot ok pulse"></span>
              <span class="route">{r.route}</span>
              <span class="spacer"></span>
              <span class="small muted">{elapsed(r.started_at, live.now)}</span>
              {#if r.permalink}<button type="button" class="link small" onclick={() => link(r.permalink)}>Open</button>{/if}
            </li>
          {/each}
        </ul>
      {/if}

      <h3 class="section">Recent</h3>
      {#if recent.length === 0}
        <p class="small muted empty">No handler runs yet.</p>
      {:else}
        <ul class="list">
          {#each recent as t (t.id)}
            <li class="row">
              <span class="badge {statusLevel(t.status)}">{t.status}</span>
              <span class="route">{t.route}</span>
              <span class="spacer"></span>
              <span class="small muted">{ago(t.finished_at ?? t.started_at, live.now)}</span>
            </li>
          {/each}
        </ul>
      {/if}

      {#if (status?.routes ?? []).some((r) => r.paused)}
        <p class="small muted paused">Paused: {status!.routes.filter((r) => r.paused).map((r) => r.name).join(", ")}</p>
      {/if}
    {/if}
    {#if message}<p class="small muted msg">{message}</p>{/if}
  </section>

  <footer class="row">
    <button onclick={() => App.OpenWindow("overview")}>Open SockDock…</button>
    <button disabled={busy || !live.snap?.running} onclick={() => act(() => App.Reload(), "Config reloaded")}>Reload</button>
    <span class="spacer"></span>
    <button class="small" title="Quit the menu bar app (the gateway keeps running)" onclick={() => App.Quit()}>Quit</button>
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
