<script lang="ts">
  import { App, errorText, type RuleRow } from "../lib/api";
  import { live } from "../lib/live.svelte";
  import { t } from "../lib/i18n/index.svelte";
  import type { MessageKey } from "../lib/i18n/en";
  import { ago, problemText, statusClass, statusLabel } from "../lib/format";

  // The rule list, grouped by channel. Within a channel, rules are tried top
  // to bottom; the handle reorders them.
  let { connectedTo, onopen, onadd }: { connectedTo: string; onopen: (name: string) => void; onadd: () => void } = $props();

  let rules = $state<RuleRow[] | null>(null);
  let error = $state("");
  let dragging = $state<string | null>(null);
  let dragStart: string[] = [];

  async function load() {
    const res = await App.ListRules();
    rules = res.rules ?? [];
    error = res.error ? t("err.config_broken", { detail: res.error }) : "";
  }
  $effect(() => {
    live.changes;
    if (!dragging) load();
  });

  const groups = $derived.by(() => {
    const m = new Map<string, RuleRow[]>();
    for (const r of rules ?? []) {
      const rows = m.get(r.form.channel) ?? [];
      rows.push(r);
      m.set(r.form.channel, rows);
    }
    // DM rules come after the channels.
    return [...m].map(([key, rows]) => ({ key, title: rows[0].channel, rows })).sort((a, b) => Number(a.key === "dm") - Number(b.key === "dm"));
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
    r.form.type === "claude" || r.form.type === "codex"
      ? t("rules.toAgent", { agent: t(r.form.type === "codex" ? "agent.codex" : "agent.claude"), folder: r.form.folder })
      : r.form.program;
  const condition = (r: RuleRow) => {
    if (r.form.mention_only && r.form.trigger === "all_posts") return t("rules.cond.mention_all");
    if (r.form.trigger === "reaction" && r.form.reaction_removed) return t("rules.cond.reaction_removed", { keyword: r.form.keyword });
    const cond = t(`rules.cond.${r.form.trigger}` as MessageKey, { keyword: r.form.keyword });
    return r.form.mention_only ? t("rules.condMention", { cond }) : cond;
  };
  const problem = (r: RuleRow) => {
    if (r.off) return "";
    if (r.problem) return problemText(r.problem);
    if (r.shadowed_by) return t(`rules.shadowed.${r.shadowed_by.trigger}` as MessageKey, { keyword: r.shadowed_by.keyword });
    return "";
  };

  // Dragging: the row under the pointer (in the same channel) swaps places
  // with the dragged one; the new order is saved on release.
  function startDrag(e: PointerEvent, r: RuleRow) {
    e.preventDefault();
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    dragging = r.form.name;
    dragStart = (rules ?? []).map((x) => x.form.name);
  }

  function moveDrag(e: PointerEvent) {
    if (!dragging || !rules) return;
    const el = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>("[data-rule]");
    const over = el?.dataset.rule;
    const me = rules.find((x) => x.form.name === dragging)!;
    const them = rules.find((x) => x.form.name === over);
    if (!el || !them || them === me || them.form.channel !== me.form.channel) return;
    const box = el.getBoundingClientRect();
    const after = e.clientY > box.top + box.height / 2;
    // Reorder within the channel and put the rules back into the channel's slots.
    const slots = rules.flatMap((x, i) => (x.form.channel === me.form.channel ? [i] : []));
    const group = slots.map((i) => rules![i]).filter((x) => x !== me);
    const at = group.indexOf(them) + (after ? 1 : 0);
    group.splice(at, 0, me);
    const next = [...rules];
    slots.forEach((i, k) => (next[i] = group[k]));
    if (next.some((x, i) => x !== rules![i])) rules = next;
  }

  async function endDrag() {
    if (!dragging || !rules) return;
    dragging = null;
    const names = rules.map((x) => x.form.name);
    if (names.join("\n") === dragStart.join("\n")) return;
    try {
      const res = await App.ReorderRules(names);
      if (!res.ok && res.config_problems?.length) error = res.config_problems.join("\n");
    } catch (err) {
      error = errorText(err);
    }
    await load();
  }
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
  {#each groups as g (g.key)}
    <div class="group">
      <div class="ghead"><strong>{g.title}</strong>{#if g.rows.length > 1}<span class="small muted">{t("rules.order")}</span>{/if}</div>
      <div class="card list">
        {#each g.rows as r (r.form.name)}
          <div class="rule" class:off={r.off} class:dragging={dragging === r.form.name} data-rule={r.form.name}
            role="button" tabindex="0" onclick={() => onopen(r.form.name)} onkeydown={(e) => e.key === "Enter" && onopen(r.form.name)}>
            <span class="handle" role="button" tabindex="-1" aria-label={t("rules.drag")}
              onpointerdown={(e) => startDrag(e, r)} onpointermove={moveDrag} onpointerup={endDrag} onpointercancel={endDrag}
              onclick={(e) => e.stopPropagation()} onkeydown={() => {}}>⠿</span>
            <div class="what">
              <div class="ln"><strong>{condition(r)}</strong> <span class="muted">→ {target(r)}</span></div>
              {#if problem(r)}<div class="small bad-t">{problem(r)}</div>{/if}
            </div>
            {#if r.off}
              <span class="last muted">{t("rules.off")}</span>
            {:else if r.last}
              <span class="last"><span class={statusClass(r.last.status)}>{statusLabel(r.last.status)}</span> · {ago(r.last.started_at, live.now)}</span>
            {/if}
            <button class="switch" class:on={!r.off} aria-label={t("rules.toggle")} aria-pressed={!r.off} onclick={(e) => toggle(r, e)}></button>
          </div>
        {/each}
      </div>
    </div>
  {/each}
  <p class="foot"><button class="link small" onclick={() => App.OpenConfigFile()}>{t("rules.openConfig")}</button></p>
{/if}

<style>
  .head { min-height: 40px; margin-bottom: 12px; }
  .note { margin: -6px 0 12px; }
  .group + .group { margin-top: 16px; }
  .ghead { display: flex; align-items: baseline; gap: 10px; margin: 0 2px 6px; }
  .list { overflow: hidden; }
  .rule { display: flex; align-items: center; gap: 12px; padding: 11px 14px 11px 8px; cursor: pointer; }
  .rule + .rule { border-top: 1px solid var(--border); }
  .rule:hover { background: color-mix(in srgb, var(--text) 3%, transparent); }
  .rule.dragging { opacity: .4; }
  .handle { cursor: grab; color: var(--muted); font-size: 14px; line-height: 1; padding: 2px; touch-action: none; }
  .what { flex: 1; min-width: 0; }
  .ln { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .rule.off .what { opacity: .45; }
  .last { font-size: 12px; white-space: nowrap; }
  .empty { text-align: center; padding: 48px 24px; display: flex; flex-direction: column; gap: 14px; align-items: center; }
  .emptyTitle { font-size: 15px; }
  .foot { margin-top: 14px; font-size: 12px; }
</style>
