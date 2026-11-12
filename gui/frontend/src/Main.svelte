<script lang="ts">
  import { Events } from "@wailsio/runtime";
  import { SockdockNavigate } from "./lib/api";
  import { live, connection } from "./lib/live.svelte";
  import Overview from "./tabs/Overview.svelte";
  import Routes from "./tabs/Routes.svelte";
  import Config from "./tabs/Config.svelte";
  import Setup from "./tabs/Setup.svelte";
  import Logs from "./tabs/Logs.svelte";
  import Prefs from "./tabs/Prefs.svelte";
  import { t } from "./lib/i18n/index.svelte";

  const tabs = [
    { id: "overview", label: "tab.overview" },
    { id: "routes", label: "tab.routes" },
    { id: "config", label: "tab.config" },
    { id: "setup", label: "tab.setup" },
    { id: "logs", label: "tab.logs" },
    { id: "prefs", label: "tab.prefs" },
  ] as const;
  type TabID = (typeof tabs)[number]["id"];

  let tab = $state<TabID>("overview");
  // A route snippet handed from Routes to Config.
  let pendingSnippet = $state("");

  Events.On(SockdockNavigate, (ev) => {
    const id = ev.data as string;
    if (tabs.some((x) => x.id === id)) tab = id as TabID;
  });

  const conn = $derived(connection(live.snap));

  function addRoute(snippet: string) {
    pendingSnippet = snippet;
    tab = "config";
  }
</script>

<div class="layout">
  <nav>
    <div class="drag"></div>
    <div class="brand">
      <strong>SockDock</strong>
      <span class="small muted">{t("app.forSlack")}</span>
    </div>
    {#each tabs as x}
      <button class="nav" class:active={tab === x.id} onclick={() => (tab = x.id)}>{t(x.label)}</button>
    {/each}
    <span class="spacer"></span>
    <div class="conn row small">
      <span class="dot {conn.level}"></span>
      <span class="muted">{conn.text}</span>
    </div>
  </nav>
  <main>
    <div class="drag"></div>
    <div class="content">
      {#if tab === "overview"}
        <Overview onnavigate={(t: string) => (tab = t as TabID)} />
      {:else if tab === "routes"}
        <Routes onaddroute={addRoute} />
      {:else if tab === "config"}
        <Config bind:snippet={pendingSnippet} />
      {:else if tab === "setup"}
        <Setup />
      {:else if tab === "logs"}
        <Logs />
      {:else}
        <Prefs />
      {/if}
    </div>
  </main>
</div>

<style>
  .layout {
    display: flex;
    height: 100vh;
  }
  nav {
    width: 190px;
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 0 10px 12px;
    background: var(--panel-2);
    border-right: 1px solid var(--border);
  }
  .drag {
    height: 40px;
    flex: none;
    --wails-draggable: drag;
  }
  .brand {
    padding: 0 8px 12px;
    display: flex;
    align-items: baseline;
    gap: 6px;
    font-size: 15px;
  }
  .nav {
    text-align: left;
    border: none;
    background: transparent;
    border-radius: 6px;
    padding: 6px 8px;
  }
  .nav:hover { background: color-mix(in srgb, var(--text) 6%, transparent); }
  .nav.active {
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
    font-weight: 500;
  }
  .conn { padding: 0 8px; align-items: flex-start; }
  .conn .dot { margin-top: 5px; }
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .content {
    flex: 1;
    overflow-y: auto;
    padding: 0 28px 28px;
  }
</style>
