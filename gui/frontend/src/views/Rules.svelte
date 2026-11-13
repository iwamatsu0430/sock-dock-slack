<script lang="ts">
  import { App, errorText, type RuleRow } from "../lib/api";
  import { live } from "../lib/live.svelte";
  import { t } from "../lib/i18n/index.svelte";
  import { ago, problemText, statusClass, statusLabel } from "../lib/format";

  let { connectedTo, onopen, onadd }: { connectedTo: string; onopen: (name: string) => void; onadd: () => void } = $props();

  let rules = $state<RuleRow[] | null>(null);
  let error = $state("");

  async function load() {
    const res = await App.ListRules();
    rules = res.rules ?? [];
    error = res.error ? t("err.config_broken", { detail: res.error }) : "";
  }
  $effect(() => {
    live.changes;
    load();
  });

  async function toggle(r: RuleRow, e: Event) {
    e.stopPropagation();
    try {
      await App.SetPaused(r.form.name, !r.off);
    } catch (err) {
      error = errorText(err);
    }
    await load();
  }

  const target = (r: RuleRow) =>
    r.form.type === "claude" ? t("rules.toAgent", { agent: t("agent.claude"), folder: r.form.folder }) : r.form.program;
</script>

<div class="row head">
  <h1>{t("rules.title")}</h1>
  <span class="spacer"></span>
  {#if rules?.length}<button class="primary" onclick={onadd}>{t("rules.add")}</button>{/if}
</div>
{#if connectedTo}<p class="small muted note">{t("setup.connected", { team: connectedTo })}</p>{/if}
{#if error}<p class="small bad-t note">{error}</p>{/if}

{#if rules && rules.length === 0}
  <div class="card empty">
    <h2 class="emptyTitle">{t("rules.emptyTitle")}</h2>
    <p class="muted">{t("rules.emptyText")}</p>
    <button class="primary" onclick={onadd}>{t("rules.add")}</button>
  </div>
{:else if rules}
  <div class="card list">
    {#each rules as r (r.form.name)}
      <div class="rule" class:off={r.off} role="button" tabindex="0" onclick={() => onopen(r.form.name)} onkeydown={(e) => e.key === "Enter" && onopen(r.form.name)}>
        <div class="what"><strong>{r.label}</strong> <span class="muted">→ {target(r)}</span></div>
        {#if r.problem}
          <span class="last bad-t">{problemText(r.problem)}</span>
        {:else if r.off}
          <span class="last muted">{t("rules.off")}</span>
        {:else if r.last}
          <span class="last"><span class={statusClass(r.last.status)}>{statusLabel(r.last.status)}</span> · {ago(r.last.started_at, live.now)}</span>
        {/if}
        <button class="switch" class:on={!r.off} aria-label={t("rules.toggle")} aria-pressed={!r.off} onclick={(e) => toggle(r, e)}></button>
      </div>
    {/each}
  </div>
  <p class="foot"><button class="link small" onclick={() => App.OpenConfigFile()}>{t("rules.openConfig")}</button></p>
{/if}

<style>
  .head { min-height: 40px; margin-bottom: 12px; }
  .note { margin: -6px 0 12px; }
  .list { overflow: hidden; }
  .rule { display: flex; align-items: center; gap: 12px; padding: 11px 14px; cursor: pointer; }
  .rule + .rule { border-top: 1px solid var(--border); }
  .rule:hover { background: color-mix(in srgb, var(--text) 3%, transparent); }
  .what { flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .rule.off .what { opacity: .45; }
  .last { font-size: 12px; white-space: nowrap; }
  .empty { text-align: center; padding: 48px 24px; display: flex; flex-direction: column; gap: 14px; align-items: center; }
  .emptyTitle { font-size: 15px; }
  .foot { margin-top: 14px; font-size: 12px; }
</style>
