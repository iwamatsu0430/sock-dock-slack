<script lang="ts">
  import { App } from "../lib/api";
  import { t } from "../lib/i18n/index.svelte";
  import Rich from "../lib/Rich.svelte";
  import TokenFields from "../lib/TokenFields.svelte";

  let { onconnected }: { onconnected: (team: string) => void } = $props();
  let step = $state(1);
</script>

<div class="setup">
  {#if step === 1}
    <p class="stepno">{t("setup.step", { n: 1 })}</p>
    <p class="big">{t("setup.createTitle")}</p>
    <p>{t("setup.createIntro")}</p>
    <ol class="todo">
      <li><div><span>{t("setup.create1")}</span><button class="small" onclick={() => App.OpenSlackAppPage()}>{t("setup.openSlack")}</button></div></li>
      <li><div><span><Rich key="setup.create2" /></span></div></li>
      <li><div><span><Rich key="setup.create3" /></span></div></li>
    </ol>
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
</style>
