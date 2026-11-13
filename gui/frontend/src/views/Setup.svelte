<script lang="ts">
  import { App, errorText } from "../lib/api";
  import { t } from "../lib/i18n/index.svelte";
  import Rich from "../lib/Rich.svelte";
  import TokenFields from "../lib/TokenFields.svelte";

  // Setup (docs/ia.md「セットアップ 1」「セットアップ 2」). Making a new app with
  // the button is the main path; an existing app's settings sit in a fold.
  let { onconnected }: { onconnected: (team: string) => void } = $props();
  let step = $state(1);
  let copied = $state(false);
  let copyError = $state("");

  async function copySettings() {
    copyError = "";
    try {
      await App.CopySlackSettings();
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch (e) {
      copyError = errorText(e);
    }
  }
</script>

<div class="setup">
  {#if step === 1}
    <p class="stepno">{t("setup.step", { n: 1 })}</p>
    <p class="big">{t("setup.createTitle")}</p>
    <p>{t("setup.createIntro")}</p>
    <ol class="todo">
      <li><div><button class="small" onclick={() => App.OpenSlackAppPage()}>{t("setup.openSlack")}</button>
        <span class="small muted">{t("setup.create1")}</span></div></li>
      <li><div><span><Rich key="setup.create2" /></span></div></li>
      <li><div><span><Rich key="setup.create3" /></span></div></li>
    </ol>
    <details class="existing">
      <summary>{t("setup.existing")}</summary>
      <p class="small">{t("setup.existingIntro")}</p>
      <table class="need small">
        <tbody>
          <tr><th>Socket Mode</th><td>{t("setup.existingOn")}</td></tr>
          <tr><th>{t("setup.existingEvents")}</th><td><code>message.channels</code> <code>message.groups</code></td></tr>
          <tr><th>{t("setup.existingScopes")}</th><td><code>channels:history</code> <code>groups:history</code> <code>chat:write</code> <code>reactions:write</code>
            <div class="muted"><Rich key="setup.existingOptional" /></div></td></tr>
        </tbody>
      </table>
      <div class="row copy">
        <button class="small" onclick={copySettings}>{copied ? t("setup.copied") : t("setup.copySettings")}</button>
        <span class="small muted">{t("setup.copyHint")}</span>
      </div>
      {#if copyError}<p class="small bad-t">{copyError}</p>{/if}
      <p class="small">{t("setup.existingSocket")}</p>
      <p class="small">{t("setup.existingReinstall")}</p>
    </details>
    <div class="row nav"><span class="spacer"></span><button class="primary" onclick={() => (step = 2)}>{t("setup.next")}</button></div>
  {:else}
    <p class="stepno">{t("setup.step", { n: 2 })}</p>
    <p class="big">{t("setup.connectTitle")}</p>
    <TokenFields backLabel={t("setup.back")} onback={() => (step = 1)} {onconnected} />
  {/if}
</div>

<style>
  .setup { max-width: 560px; margin: 0 auto; padding: 32px 44px 24px; display: flex; flex-direction: column; gap: 16px; }
  .stepno { font-size: 12px; color: var(--muted); }
  .big { font-size: 20px; font-weight: 600; }
  .nav { margin-top: 8px; }
  .todo { margin: 0; padding-left: 20px; display: flex; flex-direction: column; gap: 8px; }
  .todo li > div { display: flex; flex-direction: column; gap: 4px; align-items: flex-start; }
  details.existing { border-top: 1px solid var(--border); padding-top: 10px; display: flex; flex-direction: column; gap: 6px; }
  details.existing summary { cursor: pointer; color: var(--muted); font-size: 12px; }
  details.existing > p:first-of-type { margin-top: 8px; }
  details.existing > * + * { margin-top: 6px; }
  .need { border-collapse: collapse; width: 100%; }
  .need th { text-align: left; font-weight: 500; color: var(--muted); white-space: nowrap; vertical-align: top; padding: 4px 12px 4px 0; border-bottom: 1px solid var(--border); }
  .need td { padding: 4px 0; border-bottom: 1px solid var(--border); }
  .copy { gap: 10px; }
</style>
