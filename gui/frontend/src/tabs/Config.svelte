<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type ValidateResult } from "../lib/api";
  import { t } from "../lib/i18n/index.svelte";

  let path = $state("");
  let exists = $state(true);
  let content = $state("");
  let saved = $state("");
  let validation = $state<ValidateResult | null>(null);
  let message = $state("");
  let error = $state("");
  let saving = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  const dirty = $derived(content !== saved);

  onMount(async () => {
    try {
      const cfg = await App.GetConfig();
      path = cfg.path;
      exists = cfg.exists;
      saved = cfg.exists ? cfg.content : "";
      content = cfg.content;
      validate();
    } catch (e) {
      error = errorText(e);
    }
  });

  function validate() {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      validation = await App.ValidateConfig(content);
    }, 350);
  }

  async function save() {
    saving = true;
    error = "";
    message = "";
    try {
      const res = await App.SaveConfig(content);
      validation = res;
      if (res.ok) {
        saved = content;
        exists = true;
        message = t("config.saved");
      }
    } catch (e) {
      error = errorText(e);
    } finally {
      saving = false;
    }
  }

  function onkeydown(e: KeyboardEvent) {
    const ta = e.currentTarget as HTMLTextAreaElement;
    if (e.key === "Tab") {
      e.preventDefault();
      const { selectionStart: s, selectionEnd: end } = ta;
      content = content.slice(0, s) + "  " + content.slice(end);
      requestAnimationFrame(() => ta.setSelectionRange(s + 2, s + 2));
      validate();
    } else if ((e.metaKey || e.ctrlKey) && e.key === "s") {
      e.preventDefault();
      save();
    }
  }
</script>

<div class="wrap">
  <div class="row head">
    <h1>{t("tab.config")}</h1>
    <span class="spacer"></span>
    {#if dirty}<span class="badge warn">{t("config.unsaved")}</span>{/if}
    <button onclick={() => { content = saved; validate(); }} disabled={!dirty}>{t("config.revert")}</button>
    <button class="primary" onclick={save} disabled={saving || !dirty || (validation !== null && !validation.ok)}>{t("config.save")}</button>
  </div>
  <p class="small muted">{t("config.advancedNote")}</p>
  <p class="small muted path">
    {path}
  </p>

  <textarea class="code editor" spellcheck="false" bind:value={content} oninput={validate} {onkeydown}></textarea>

  {#if validation && !validation.ok}
    <div class="error-box">
      {#each validation.problems ?? [] as p}<div>• {p}</div>{/each}
    </div>
  {:else if validation?.ok}
    <p class="small" style="color: var(--ok)">{t("config.valid")}</p>
  {/if}
  {#if message}<div class="ok-box">{message}</div>{/if}
  {#if error}<div class="error-box">{error}</div>{/if}
</div>

<style>
  .wrap {
    display: flex;
    flex-direction: column;
    gap: 10px;
    height: calc(100vh - 68px);
  }
  .head { min-height: 32px; }
  .path { margin-top: -6px; user-select: text; }
  .editor {
    flex: 1;
    min-height: 240px;
    resize: none;
    padding: 12px;
    background: var(--code-bg);
    tab-size: 2;
    white-space: pre;
  }
</style>
