<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type ServiceInfo, type TokenInfo, type TokenTest } from "../lib/api";
  import { live, gatewayText } from "../lib/live.svelte";
  import { t } from "../lib/i18n/index.svelte";
  import Rich from "../lib/Rich.svelte";

  let { onnavigate }: { onnavigate: (tab: string) => void } = $props();

  const slackApps = "https://api.slack.com/apps?new_app=1";
  const APP = "SLACK_APP_TOKEN";
  const BOT = "SLACK_BOT_TOKEN";

  let tokens = $state<TokenInfo[]>([]);
  let input = $state<Record<string, string>>({ [APP]: "", [BOT]: "" });
  let tests = $state<Record<string, TokenTest>>({});
  let tokenMsg = $state("");
  let tokenErr = $state("");
  let svc = $state<ServiceInfo | null>(null);
  let launchAtLogin = $state(false);
  let gwErr = $state("");
  let busy = $state(false);
  let claude = $state<string[] | null>(null);
  let copied = $state(false);

  const gw = $derived(live.snap?.gateway);
  const info = (name: string) => tokens.find((x) => x.name === name);
  const anyTokenSet = $derived(tokens.some((x) => x.set));
  const tokensReady = $derived(tokens.length > 0 && tokens.every((x) => x.set));

  async function refresh() {
    tokens = (await App.TokenStatus()) ?? [];
    svc = await App.ServiceStatus();
    launchAtLogin = await App.LaunchAtLogin();
  }
  onMount(refresh);

  async function run(fn: () => Promise<unknown>) {
    busy = true;
    try {
      await fn();
    } finally {
      busy = false;
    }
  }

  function saveTokens() {
    tokenErr = tokenMsg = "";
    return run(async () => {
      try {
        for (const name of [APP, BOT]) {
          if (input[name].trim()) await App.SaveToken(name, input[name]);
        }
        input = { [APP]: "", [BOT]: "" };
        tokenMsg = t("setup.saved");
        tests = {};
        await refresh();
      } catch (e) {
        tokenErr = errorText(e);
      }
    });
  }

  function testTokens() {
    tokenErr = tokenMsg = "";
    return run(async () => {
      const results = (await App.TestTokens()) ?? [];
      tests = Object.fromEntries(results.map((r) => [r.name, r]));
    });
  }

  function testText(name: string): { level: string; text: string } | null {
    const r = tests[name];
    if (!r) return null;
    if (!r.set) return { level: "warn", text: name === APP ? t("setup.testMissingApp") : t("setup.testMissingBot") };
    if (!r.ok) return { level: "bad", text: t("setup.testFailed", { error: r.error ?? "" }) };
    return { level: "ok", text: name === APP ? t("setup.testOkApp") : t("setup.testOkBot", { detail: r.detail ?? "" }) };
  }

  async function setLaunch(on: boolean) {
    gwErr = "";
    try {
      await App.SetLaunchAtLogin(on);
      launchAtLogin = on;
    } catch (e) {
      gwErr = errorText(e);
      launchAtLogin = await App.LaunchAtLogin();
    }
  }

  function gateway(fn: () => Promise<unknown>) {
    gwErr = "";
    return run(async () => {
      try {
        await fn();
      } catch (e) {
        gwErr = errorText(e);
      }
      svc = await App.ServiceStatus();
    });
  }

  async function copyManifest() {
    copied = await App.CopyManifest();
    setTimeout(() => (copied = false), 2000);
  }
</script>

<div class="stack">
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
    <p class="small muted">{t("setup.bothNeeded")}</p>
    {#each [{ name: APP, label: t("setup.appToken"), role: t("setup.appTokenRole"), ph: "xapp-…" }, { name: BOT, label: t("setup.botToken"), role: t("setup.botTokenRole"), ph: "xoxb-…" }] as row (row.name)}
      {@const ti = info(row.name)}
      {@const tr = testText(row.name)}
      <div class="token">
        <div class="row">
          <label for={"t-" + row.name}>{row.label}</label>
          {#if ti?.error}<span class="badge bad">{t("setup.tokenError")}</span>
          {:else if ti?.set}<span class="badge ok">{t("setup.tokenSet")}</span> <span class="small muted">{t("setup.tokenFrom", { preview: ti.preview, source: ti.source })}</span>
          {:else}<span class="badge warn">{t("setup.tokenNotSet")}</span>{/if}
        </div>
        <p class="small muted">{row.role}</p>
        <input id={"t-" + row.name} type="password" bind:value={input[row.name]} placeholder={row.ph} autocomplete="off" />
        {#if ti?.error}<p class="small bad-text">{ti.error}</p>{/if}
        {#if tr}<p class="small {tr.level}-text">{tr.text}</p>{/if}
      </div>
    {/each}
    <div class="row">
      <button class="primary" disabled={busy || (!input[APP].trim() && !input[BOT].trim())} onclick={saveTokens}>{t("setup.saveKeychain")}</button>
      <button disabled={busy || !anyTokenSet} onclick={testTokens}>{t("setup.testTokens")}</button>
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
    <div class="row"><span class="step">4</span><h2>{t("setup.step4")}</h2></div>
    <p class="muted small">{t("setup.step4Help")}</p>
    <label class="row toggle">
      <input type="checkbox" checked={launchAtLogin} onchange={(e) => setLaunch(e.currentTarget.checked)} />
      <span>{t("setup.launchAtLogin")}</span>
    </label>
    <p class="small muted">{t("setup.quitNote")}</p>

    <div class="row gw">
      <span class="small">{t("setup.gatewayStatus")}</span>
      <span class="dot {gw?.state === 'running' || gw?.state === 'external' ? 'ok' : gw?.state === 'error' ? 'bad' : 'idle'}"></span>
      <span class="small">{gatewayText(gw)}</span>
      <span class="spacer"></span>
      {#if gw?.state === "running"}
        <button class="small" disabled={busy} onclick={() => gateway(() => App.RestartGateway())}>{t("gw.restart")}</button>
        <button class="small" disabled={busy} onclick={() => gateway(() => App.StopGateway())}>{t("gw.stop")}</button>
      {:else if gw?.state === "setup" && gw.missing === "config"}
        <button class="small" onclick={() => onnavigate("config")}>{t("gw.openConfig")}</button>
      {:else if gw?.state === "stopped" || gw?.state === "error"}
        <button class="small primary" disabled={busy} onclick={() => gateway(() => App.StartGateway())}>{t("gw.start")}</button>
      {/if}
    </div>
    {#if gw?.state === "external" && svc?.installed}
      <p class="small muted"><Rich key="setup.serviceInstalled" /></p>
      <div class="row">
        <button class="small" disabled={busy} onclick={() => gateway(() => App.UninstallService())}>{t("setup.removeService")}</button>
      </div>
    {/if}
    {#if gwErr}<div class="error-box">{gwErr}</div>{/if}
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
  .token {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 10px 0;
    border-top: 1px solid var(--border);
  }
  .token label { margin: 0; }
  .toggle { font-weight: normal; margin: 0; gap: 8px; }
  .gw {
    padding: 8px 10px;
    background: var(--panel-2);
    border-radius: 6px;
  }
  .ok-text { color: var(--ok); }
  .warn-text { color: var(--warn); }
  .bad-text { color: var(--bad); }
</style>
