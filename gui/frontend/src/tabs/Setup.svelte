<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type ServiceInfo, type TokenInfo } from "../lib/api";
  import { live } from "../lib/live.svelte";
  import { t } from "../lib/i18n/index.svelte";
  import Rich from "../lib/Rich.svelte";

  const slackApps = "https://api.slack.com/apps?new_app=1";

  let tokens = $state<TokenInfo[]>([]);
  let appToken = $state("");
  let botToken = $state("");
  let tokenMsg = $state("");
  let tokenErr = $state("");
  let svc = $state<ServiceInfo | null>(null);
  let svcMsg = $state("");
  let svcErr = $state("");
  let busy = $state(false);
  let claude = $state<string[] | null>(null);
  let copied = $state(false);

  async function refresh() {
    tokens = (await App.TokenStatus()) ?? [];
    svc = await App.ServiceStatus();
  }
  onMount(refresh);

  async function saveTokens() {
    tokenErr = tokenMsg = "";
    busy = true;
    try {
      if (appToken.trim()) await App.SaveToken("SLACK_APP_TOKEN", appToken);
      if (botToken.trim()) await App.SaveToken("SLACK_BOT_TOKEN", botToken);
      appToken = botToken = "";
      tokenMsg = t("setup.saved");
      await refresh();
    } catch (e) {
      tokenErr = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function testConnection() {
    tokenErr = tokenMsg = "";
    busy = true;
    try {
      const id = await App.TestConnection();
      tokenMsg = t("setup.connectedAs", { user: id.user_id, team: id.team });
    } catch (e) {
      tokenErr = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function service(fn: () => Promise<unknown>, done: string) {
    svcErr = svcMsg = "";
    busy = true;
    try {
      await fn();
      svcMsg = done;
    } catch (e) {
      svcErr = errorText(e);
    } finally {
      busy = false;
      svc = await App.ServiceStatus();
    }
  }

  async function copyManifest() {
    copied = await App.CopyManifest();
    setTimeout(() => (copied = false), 2000);
  }

  const tokensReady = $derived(tokens.length > 0 && tokens.every((x) => x.set));
</script>

<div class="stack">
  <h1>{t("tab.setup")}</h1>

  <section class="card stack">
    <div class="row"><span class="step">1</span><h2>{t("setup.step1")}</h2></div>
    <p class="muted">{t("setup.step1Help")}</p>
    <ol class="small">
      <li>
        <Rich key="setup.step1a">
          {#snippet slot()}<button type="button" class="link" onclick={() => App.OpenURL(slackApps)}>api.slack.com/apps</button>{/snippet}
        </Rich>
      </li>
      <li><Rich key="setup.step1b" /></li>
      <li><Rich key="setup.step1c" /></li>
      <li><Rich key="setup.step1d" /></li>
    </ol>
    <div class="row">
      <button onclick={copyManifest}>{copied ? t("setup.copied") : t("setup.copyManifest")}</button>
      <button onclick={() => App.OpenURL(slackApps)}>{t("setup.openSlack")}</button>
    </div>
  </section>

  <section class="card stack">
    <div class="row"><span class="step">2</span><h2>{t("setup.step2")}</h2>{#if tokensReady}<span class="badge ok">{t("setup.ready")}</span>{/if}</div>
    <table>
      <tbody>
        {#each tokens as x (x.name)}
          <tr>
            <td class="mono">{x.name}</td>
            <td>
              {#if x.error}<span class="badge bad">{t("setup.tokenError")}</span> <span class="small">{x.error}</span>
              {:else if x.set}<span class="badge ok">{t("setup.tokenSet")}</span> <span class="small muted">{t("setup.tokenFrom", { preview: x.preview, source: x.source })}</span>
              {:else}<span class="badge">{t("setup.tokenNotSet")}</span>{/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
    <div class="grid2">
      <div><label for="t-app">{t("setup.appToken")}</label><input id="t-app" type="password" bind:value={appToken} placeholder="xapp-…" autocomplete="off" /></div>
      <div><label for="t-bot">{t("setup.botToken")}</label><input id="t-bot" type="password" bind:value={botToken} placeholder="xoxb-…" autocomplete="off" /></div>
    </div>
    <div class="row">
      <button class="primary" disabled={busy || (!appToken.trim() && !botToken.trim())} onclick={saveTokens}>{t("setup.saveKeychain")}</button>
      <button disabled={busy || !tokensReady} onclick={testConnection}>{t("setup.testConnection")}</button>
    </div>
    <p class="small muted">{t("setup.envPrecedence")}</p>
    {#if tokenMsg}<div class="ok-box">{tokenMsg}</div>{/if}
    {#if tokenErr}<div class="error-box">{tokenErr}</div>{/if}
  </section>

  <section class="card stack">
    <div class="row"><span class="step">3</span><h2>{t("setup.step3")}</h2></div>
    <p class="muted small"><Rich key="setup.step3Help" /></p>
    <div class="row">
      <button onclick={async () => (claude = await App.DetectClaude())}>{t("setup.findClaude")}</button>
    </div>
    {#if claude}
      {#if claude.length === 0}
        <p class="small muted">{t("setup.claudeNotFound")}</p>
      {:else}
        <ul class="small paths">
          {#each claude as p}<li><code>{p}</code></li>{/each}
        </ul>
      {/if}
    {/if}
  </section>

  <section class="card stack">
    <div class="row"><span class="step">4</span><h2>{t("setup.step4")}</h2>{#if live.snap?.running}<span class="badge ok">{t("setup.running")}</span>{/if}</div>
    <p class="muted small">{t("setup.step4Help")}</p>
    {#if svc}
      <p class="small">{t("setup.serviceStatus")} <strong>{svc.detail}</strong></p>
      <p class="small muted">{t("setup.binary")} <code>{svc.binary || t("setup.binaryMissing")}</code></p>
    {/if}
    <div class="row">
      <button class="primary" disabled={busy} onclick={() => service(() => App.InstallService(), t("setup.installed"))}>
        {svc?.installed ? t("setup.reinstall") : t("setup.install")}
      </button>
      <button disabled={busy || !svc?.installed} onclick={() => service(() => App.RestartService(), t("setup.restarted"))}>{t("setup.restart")}</button>
      <button class="danger" disabled={busy || !svc?.installed} onclick={() => service(() => App.UninstallService(), t("setup.removed"))}>{t("setup.uninstall")}</button>
    </div>
    {#if svcMsg}<div class="ok-box">{svcMsg}</div>{/if}
    {#if svcErr}<div class="error-box">{svcErr}</div>{/if}
  </section>
</div>

<style>
  .step {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-weight: 600;
    font-size: 12px;
    flex: none;
  }
  ol { margin: 0; padding-left: 20px; display: flex; flex-direction: column; gap: 4px; }
  .paths { margin: 0; padding-left: 18px; user-select: text; }
</style>
