<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type ChannelList, type MatchResult, type RouteForm } from "../lib/api";
  import { live } from "../lib/live.svelte";
  import { t } from "../lib/i18n/index.svelte";
  import RouteEditor from "../lib/RouteEditor.svelte";

  let { startAdding = false }: { startAdding?: boolean } = $props();

  let routes = $state<RouteForm[]>([]);
  let channels = $state<ChannelList | null>(null);
  let editing = $state<RouteForm | null>(null);
  let message = $state("");
  let error = $state("");

  const blank = (): RouteForm =>
    ({
      original_name: "", name: "", channel: "", trigger: "keyword", keyword: "", regex: "", type: "claude", folder: "",
      instructions: "", allow_web: false, allow_edit: false, commands: [], other_tools: [], model: "", prompt_file: "",
      program: "", args: "", timeout_sec: 0,
    }) as RouteForm;

  async function load() {
    try {
      routes = (await App.ListRoutes()) ?? [];
      error = "";
    } catch (e) {
      error = errorText(e);
    }
  }
  async function loadChannels() {
    channels = await App.ListChannels();
  }

  onMount(() => {
    load();
    loadChannels();
    if (startAdding) editing = blank();
  });

  const paused = (name: string) => live.snap?.status?.routes?.find((r) => r.name === name)?.paused ?? false;
  const channelName = (id: string) => {
    const c = channels?.channels?.find((x) => x.id === id);
    return c ? `${c.private ? "🔒" : "#"}${c.name}` : id;
  };

  async function remove(r: RouteForm) {
    if (!confirm(t("routes.deleteConfirm", { name: r.name }))) return;
    const res = await App.DeleteRoute(r.name);
    if (!res.ok) error = (res.config_problems ?? []).join("\n");
    await load();
  }

  async function saved(msg: string) {
    editing = null;
    message = msg;
    await load();
  }

  // ---- tester ----
  let testChannel = $state("");
  let testText = $state("");
  let result = $state<MatchResult | null>(null);
  let testError = $state("");
  async function test() {
    testError = "";
    result = null;
    try {
      const cfg = await App.GetConfig();
      result = await App.MatchInContent(cfg.content, testChannel.trim(), testText);
    } catch (e) {
      testError = errorText(e);
    }
  }
</script>

<div class="stack">
  <div class="row head">
    <h1>{t("tab.routes")}</h1>
    <span class="spacer"></span>
    {#if !editing}<button class="primary" onclick={() => ((editing = blank()), (message = ""))}>{t("routes.add")}</button>{/if}
  </div>
  <p class="muted">{t("routes.intro")}</p>

  {#if message}<div class="ok-box">{message}</div>{/if}
  {#if error}<div class="error-box">{error}</div>{/if}

  {#if editing && !editing.original_name}
    <RouteEditor initial={editing} {channels} onreloadchannels={loadChannels} onsaved={saved} oncancel={() => (editing = null)} />
  {/if}

  {#if routes.length === 0 && !editing}
    <div class="card stack empty">
      <h2>{t("routes.empty")}</h2>
      <p class="muted">{t("routes.emptyHelp")}</p>
      <div><button class="primary" onclick={() => (editing = blank())}>{t("routes.add")}</button></div>
    </div>
  {/if}

  {#each routes as r (r.original_name)}
    {#if editing?.original_name === r.original_name}
      <RouteEditor initial={editing} {channels} onreloadchannels={loadChannels} onsaved={saved} oncancel={() => (editing = null)} />
    {:else}
      <div class="card route">
        <div class="row">
          <strong>{r.name}</strong>
          {#if paused(r.name)}<span class="badge warn">{t("routes.paused")}</span>{/if}
          <span class="spacer"></span>
          <button class="small" onclick={() => App.SetPaused(r.name, !paused(r.name))}>{paused(r.name) ? t("action.resume") : t("action.pause")}</button>
          <button class="small" onclick={() => ((editing = r), (message = ""))}>{t("routes.edit")}</button>
          <button class="small danger" onclick={() => remove(r)}>{t("routes.delete")}</button>
        </div>
        <p class="summary">
          {t("routes.summaryWhere", { channel: channelName(r.channel) })}
          {r.trigger === "keyword" ? t("routes.triggerKeyword", { keyword: r.keyword }) : t("routes.triggerRegex", { regex: r.regex })}
          →
          {r.type === "claude" ? t("routes.actionClaude", { folder: r.folder }) : t("routes.actionCommand", { program: r.program })}
        </p>
      </div>
    {/if}
  {/each}

  {#if routes.length > 0}
    <div class="card stack">
      <h2>{t("routes.testTitle")}</h2>
      <p class="small muted">{t("routes.testHelp")}</p>
      <div class="row">
        {#if channels?.channels?.length}
          <select bind:value={testChannel} style="max-width: 200px" aria-label={t("routes.channelPlaceholder")}>
            {#each channels.channels as c (c.id)}<option value={c.id}>{c.private ? "🔒" : "#"} {c.name}</option>{/each}
          </select>
        {:else}
          <input type="text" placeholder={t("routes.channelPlaceholder")} bind:value={testChannel} style="max-width: 180px" />
        {/if}
        <input type="text" placeholder={t("routes.textPlaceholder")} bind:value={testText} onkeydown={(e) => e.key === "Enter" && test()} />
        <button onclick={test}>{t("routes.test")}</button>
      </div>
      {#if testError}<div class="error-box">{testError}</div>{/if}
      {#if result}
        {#if result.matched}
          <div class="ok-box">
            {t("routes.matchedRoute")} <strong>{result.route}</strong>
            {#each Object.entries(result.groups ?? {}) as [k, v]}
              <span class="small"> · {k} = <code>{v}</code></span>
            {/each}
          </div>
        {:else}
          <p class="muted">{t("routes.noMatch")}</p>
        {/if}
      {/if}
    </div>
  {/if}
</div>

<style>
  .head { min-height: 32px; }
  .route .summary { margin-top: 6px; color: var(--muted); }
  .empty { align-items: flex-start; }
</style>
