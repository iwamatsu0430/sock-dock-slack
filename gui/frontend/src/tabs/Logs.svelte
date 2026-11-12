<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { App, errorText } from "../lib/api";
  import { t } from "../lib/i18n/index.svelte";

  let lines = $state<string[]>([]);
  let filter = $state("");
  let follow = $state(true);
  let error = $state("");
  let box: HTMLPreElement | undefined = $state();
  let timer: ReturnType<typeof setInterval> | undefined;

  const shown = $derived(filter ? lines.filter((l) => l.toLowerCase().includes(filter.toLowerCase())) : lines);

  async function load() {
    try {
      lines = (await App.Logs(500)) ?? [];
      error = "";
      if (follow) requestAnimationFrame(() => box && (box.scrollTop = box.scrollHeight));
    } catch (e) {
      error = errorText(e);
    }
  }

  onMount(() => {
    load();
    timer = setInterval(() => follow && load(), 2000);
  });
  onDestroy(() => clearInterval(timer));

  function level(l: string) {
    if (l.includes("level=ERROR")) return "err";
    if (l.includes("level=WARN")) return "warn";
    return "";
  }
</script>

<div class="wrap">
  <div class="row head">
    <h1>{t("tab.logs")}</h1>
    <span class="spacer"></span>
    <input type="text" placeholder={t("logs.filter")} bind:value={filter} style="max-width: 220px" />
    <label class="row follow"><input type="checkbox" bind:checked={follow} /> {t("logs.follow")}</label>
    <button onclick={load}>{t("action.refresh")}</button>
    <button onclick={() => App.RevealLogs()}>{t("logs.reveal")}</button>
  </div>
  {#if error}<div class="error-box">{error}</div>{/if}
  <pre class="log" bind:this={box}>{#each shown as l}<span class={level(l)}>{l}</span>
{/each}</pre>
</div>

<style>
  .wrap { display: flex; flex-direction: column; gap: 10px; height: calc(100vh - 68px); }
  .head { min-height: 32px; }
  .follow { margin: 0; font-weight: normal; gap: 4px; }
  .log {
    flex: 1;
    margin: 0;
    overflow: auto;
    padding: 10px 12px;
    background: var(--code-bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    font-size: 11.5px;
    user-select: text;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .err { color: var(--bad); }
  .warn { color: var(--warn); }
</style>
