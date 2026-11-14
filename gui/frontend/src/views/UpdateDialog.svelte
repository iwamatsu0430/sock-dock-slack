<script lang="ts">
  import { App, errorText } from "../lib/api";
  import { live } from "../lib/live.svelte";
  import { i18n, t } from "../lib/i18n/index.svelte";

  // Before updating: the version, the release notes in
  // the app's language, and whether a running rule will be stopped.
  let { onclose }: { onclose: () => void } = $props();
  let error = $state("");
  let busy = $state(false);

  const update = $derived(live.snap?.status?.update ?? null);
  const running = $derived((live.snap?.status?.running ?? []).length > 0);
  const notes = $derived(update ? (update.notes?.[i18n.locale] ?? update.notes?.en ?? "") : "");

  // The release notes come from GitHub, so they are never put in as HTML:
  // only list items, `code` and **bold** become parts; the rest is text.
  type Part = { kind: "text" | "code" | "bold"; text: string };
  type Line = { item: boolean; parts: Part[] };
  function inline(s: string): Part[] {
    const out: Part[] = [];
    // Links show their text only.
    s = s.replace(/\[([^\]]+)\]\([^)]*\)/g, "$1");
    for (const piece of s.split(/(`[^`]+`|\*\*[^*]+\*\*)/)) {
      if (!piece) continue;
      if (piece.startsWith("`") && piece.endsWith("`")) out.push({ kind: "code", text: piece.slice(1, -1) });
      else if (piece.startsWith("**") && piece.endsWith("**")) out.push({ kind: "bold", text: piece.slice(2, -2) });
      else out.push({ kind: "text", text: piece });
    }
    return out;
  }
  const lines = $derived(
    notes
      .split("\n")
      .map((l) => l.trimEnd())
      .filter((l) => l.trim() !== "" && !l.startsWith("#"))
      .map((l): Line => {
        const m = l.match(/^\s*[-*]\s+(.*)$/);
        return m ? { item: true, parts: inline(m[1]) } : { item: false, parts: inline(l) };
      }),
  );

  async function start() {
    busy = true;
    error = "";
    try {
      await App.StartUpdate();
      onclose();
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

{#if update}
  <div class="overlay" role="presentation">
    <div class="dialog" role="dialog" aria-labelledby="update-title">
      <p class="big" id="update-title">SockDock {update.version}</p>
      {#if lines.length}
        <div class="notes">
          {#each lines as line, i (i)}
            {#if line.item}<div class="li">{#each line.parts as p, j (j)}{#if p.kind === "code"}<code>{p.text}</code>{:else if p.kind === "bold"}<strong>{p.text}</strong>{:else}{p.text}{/if}{/each}</div>
            {:else}<p>{#each line.parts as p, j (j)}{#if p.kind === "code"}<code>{p.text}</code>{:else if p.kind === "bold"}<strong>{p.text}</strong>{:else}{p.text}{/if}{/each}</p>{/if}
          {/each}
        </div>
      {/if}
      <p class="small"><button class="link small" onclick={() => App.OpenURL(update.url)}>{t("update.github")}</button></p>
      {#if running}<p class="small bad-t">{t("update.running")}</p>{/if}
      {#if error}<p class="small bad-t">{error}</p>{/if}
      <div class="row"><span class="spacer"></span>
        <button disabled={busy} onclick={onclose}>{t("update.later")}</button>
        <button class="primary" disabled={busy} onclick={start}>{t("update.update")}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay { position: fixed; inset: 0; background: rgba(0, 0, 0, .28); display: flex; align-items: flex-start; justify-content: center; padding-top: 70px; z-index: 20; }
  .dialog { width: 420px; max-width: calc(100% - 32px); background: var(--panel); border: 1px solid var(--border); border-radius: 10px; padding: 18px 20px;
    display: flex; flex-direction: column; gap: 10px; box-shadow: 0 16px 40px rgba(0, 0, 0, .3); }
  .big { font-size: 15px; font-weight: 600; }
  /* One size smaller and tighter than body text. */
  .notes { max-height: 150px; overflow-y: auto; border: 1px solid var(--border); border-radius: 6px; padding: 5px 10px; background: var(--bg);
    font-size: 11px; line-height: 1.4; display: flex; flex-direction: column; gap: 1px; }
  .notes p { margin: 0; }
  .li { position: relative; padding-left: 12px; }
  .li::before { content: "•"; position: absolute; left: 0; }
  .notes code { font-size: 10.5px; }
</style>
