<script lang="ts">
  import { App } from "../lib/api";
  import { t } from "../lib/i18n/index.svelte";
  import type { MessageKey } from "../lib/i18n/en";
  import Rich from "../lib/Rich.svelte";
  import TokenFields from "../lib/TokenFields.svelte";

  let { onconnected }: { onconnected: (team: string) => void } = $props();
  let step = $state(1);

  // The Bot Token scopes in the manifest and what each is for; the last two
  // are optional (docs/ia.md「セットアップ 1」).
  const scopes: [string, MessageKey][] = [
    ["channels:history", "setup.scope.channelsHistory"],
    ["groups:history", "setup.scope.groupsHistory"],
    ["chat:write", "setup.scope.chatWrite"],
    ["reactions:write", "setup.scope.reactionsWrite"],
    ["channels:read", "setup.scope.channelsRead"],
    ["groups:read", "setup.scope.groupsRead"],
  ];
</script>

<div class="setup">
  {#if step === 1}
    <p class="stepno">{t("setup.step", { n: 1 })}</p>
    <p class="big">{t("setup.createTitle")}</p>
    <p>{t("setup.createIntro")}</p>
    <ol class="todo">
      <li><div><span>{t("setup.create1")}</span><button class="small" onclick={() => App.OpenSlackAppPage()}>{t("setup.openSlack")}</button></div></li>
      <li><div><span><Rich key="setup.create2" /></span>
        <ul class="scopes small">
          {#each scopes as [scope, key] (scope)}<li><code>{scope}</code><span>{t(key)}</span></li>{/each}
        </ul></div></li>
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
  .scopes { list-style: none; margin: 2px 0 0; padding: 0; display: grid; grid-template-columns: max-content 1fr; gap: 4px 12px;
    align-items: baseline; color: var(--muted); }
  .scopes li { display: contents; }
  .scopes code { justify-self: start; }
</style>
