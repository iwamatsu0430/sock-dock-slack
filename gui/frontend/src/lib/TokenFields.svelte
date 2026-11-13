<script lang="ts">
  import { App, errorText, type TokenProblem } from "./api";
  import { t } from "./i18n/index.svelte";
  import Rich from "./Rich.svelte";

  // The two token fields and 「Slack に接続」. Nothing is checked or saved
  // until the button is pressed (docs/ia.md).
  let {
    backLabel,
    onback,
    onconnected,
  }: { backLabel: string; onback: () => void; onconnected: (team: string) => void } = $props();

  let appToken = $state("");
  let botToken = $state("");
  let appProblem = $state<TokenProblem | null>(null);
  let botProblem = $state<TokenProblem | null>(null);
  let busy = $state(false);

  async function connect() {
    busy = true;
    appProblem = botProblem = null;
    try {
      const res = await App.ConnectSlack(appToken, botToken);
      appProblem = res.app ?? null;
      botProblem = res.bot ?? null;
      if (res.ok) {
        appToken = botToken = "";
        onconnected(res.team ?? "");
      }
    } catch (e) {
      botProblem = { code: "network", detail: errorText(e) };
    } finally {
      busy = false;
    }
  }
</script>

{#snippet problem(p: TokenProblem, kind: "app" | "bot")}
  <p class="small bad-t hint">
    {#if p.code === "format"}<Rich key={kind === "app" ? "token.formatApp" : "token.formatBot"} />
    {:else if p.code === "missing_scope" && kind === "app"}<Rich key="token.missingScope" />
    {:else if p.code === "network"}{t("token.network", { detail: p.detail ?? "" })}
    {:else if p.code === "keychain"}{t("token.keychainError", { detail: p.detail ?? "" })}
    {:else}{t("token.auth", { code: p.code })}{/if}
  </p>
{/snippet}

<div>
  <label class="f" for="t-app">{t("token.app")}</label>
  <input id="t-app" type="password" placeholder="xapp-…" bind:value={appToken} class:invalid={appProblem} autocomplete="off" />
  {#if appProblem}{@render problem(appProblem, "app")}{:else}<p class="small muted hint">{t("token.appHint")}</p>{/if}
</div>
<div>
  <label class="f" for="t-bot">{t("token.bot")}</label>
  <input id="t-bot" type="password" placeholder="xoxb-…" bind:value={botToken} class:invalid={botProblem} autocomplete="off" />
  {#if botProblem}{@render problem(botProblem, "bot")}{:else}<p class="small muted hint">{t("token.botHint")}</p>{/if}
</div>
<p class="small muted">{t("token.keychain")}</p>
<div class="row nav">
  <button onclick={onback}>{backLabel}</button>
  <span class="spacer"></span>
  <button class="primary" disabled={busy || !appToken.trim() || !botToken.trim()} onclick={connect}>{t("token.connect")}</button>
</div>

<style>
  .hint { margin-top: 3px; }
  .nav { margin-top: 8px; }
</style>
