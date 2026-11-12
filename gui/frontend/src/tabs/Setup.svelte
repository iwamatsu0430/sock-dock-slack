<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type ServiceInfo, type TokenInfo } from "../lib/api";
  import { live } from "../lib/live.svelte";

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
      tokenMsg = "Saved to the Keychain.";
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
      tokenMsg = `Connected as ${id.user_id} on ${id.team}.`;
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

  const tokensReady = $derived(tokens.length > 0 && tokens.every((t) => t.set));
</script>

<div class="stack">
  <h1>Setup</h1>

  <section class="card stack">
    <div class="row"><span class="step">1</span><h2>Create the Slack app</h2></div>
    <p class="muted">
      SockDock uses one Slack app with Socket Mode, so nothing needs to be reachable from the internet. Create the app from the bundled manifest, then
      install it to your workspace.
    </p>
    <ol class="small">
      <li>Copy the manifest, open <button type="button" class="link" onclick={() => App.OpenURL("https://api.slack.com/apps?new_app=1")}>api.slack.com/apps</button> and choose <em>From a manifest</em>.</li>
      <li><em>Basic Information → App-Level Tokens</em>: generate a token with <code>connections:write</code> (starts with <code>xapp-</code>).</li>
      <li><em>Install App</em>: install to your workspace and copy the <em>Bot User OAuth Token</em> (starts with <code>xoxb-</code>).</li>
      <li>Invite the bot to each channel you want to route from: <code>/invite @SockDock</code>.</li>
    </ol>
    <div class="row">
      <button onclick={copyManifest}>{copied ? "Copied ✓" : "Copy manifest"}</button>
      <button onclick={() => App.OpenURL("https://api.slack.com/apps?new_app=1")}>Open Slack</button>
    </div>
  </section>

  <section class="card stack">
    <div class="row"><span class="step">2</span><h2>Tokens</h2>{#if tokensReady}<span class="badge ok">ready</span>{/if}</div>
    <table>
      <tbody>
        {#each tokens as t (t.name)}
          <tr>
            <td class="mono">{t.name}</td>
            <td>
              {#if t.error}<span class="badge bad">error</span> <span class="small">{t.error}</span>
              {:else if t.set}<span class="badge ok">set</span> <span class="small muted">{t.preview} · from {t.source}</span>
              {:else}<span class="badge">not set</span>{/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
    <div class="grid2">
      <div><label for="t-app">App-level token</label><input id="t-app" type="password" bind:value={appToken} placeholder="xapp-…" autocomplete="off" /></div>
      <div><label for="t-bot">Bot token</label><input id="t-bot" type="password" bind:value={botToken} placeholder="xoxb-…" autocomplete="off" /></div>
    </div>
    <div class="row">
      <button class="primary" disabled={busy || (!appToken.trim() && !botToken.trim())} onclick={saveTokens}>Save to Keychain</button>
      <button disabled={busy || !tokensReady} onclick={testConnection}>Test connection</button>
    </div>
    <p class="small muted">Tokens set as environment variables take precedence over the Keychain.</p>
    {#if tokenMsg}<div class="ok-box">{tokenMsg}</div>{/if}
    {#if tokenErr}<div class="error-box">{tokenErr}</div>{/if}
  </section>

  <section class="card stack">
    <div class="row"><span class="step">3</span><h2>Claude Code (for claude routes)</h2></div>
    <p class="muted small">
      The background service does not run your shell, so version-manager shims may fail there. Set <code>settings.claude_command</code> in Config to an
      absolute path.
    </p>
    <div class="row">
      <button onclick={async () => (claude = await App.DetectClaude())}>Find Claude Code</button>
    </div>
    {#if claude}
      {#if claude.length === 0}
        <p class="small muted">Not found. Install Claude Code, or set the path yourself.</p>
      {:else}
        <ul class="small paths">
          {#each claude as p}<li><code>{p}</code></li>{/each}
        </ul>
      {/if}
    {/if}
  </section>

  <section class="card stack">
    <div class="row"><span class="step">4</span><h2>Background service</h2>{#if live.snap?.running}<span class="badge ok">running</span>{/if}</div>
    <p class="muted small">
      Runs the gateway at login and restarts it if it stops. It uses your login shell's PATH so handlers find the same tools as your terminal.
    </p>
    {#if svc}
      <p class="small">Status: <strong>{svc.detail}</strong></p>
      <p class="small muted">Binary: <code>{svc.binary || "sockdock not found"}</code></p>
    {/if}
    <div class="row">
      <button class="primary" disabled={busy} onclick={() => service(() => App.InstallService(), "Service installed and started.")}>
        {svc?.installed ? "Reinstall" : "Install & start"}
      </button>
      <button disabled={busy || !svc?.installed} onclick={() => service(() => App.RestartService(), "Restarted.")}>Restart</button>
      <button class="danger" disabled={busy || !svc?.installed} onclick={() => service(() => App.UninstallService(), "Service removed.")}>Uninstall</button>
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
  }
  ol { margin: 0; padding-left: 20px; display: flex; flex-direction: column; gap: 4px; }
  .paths { margin: 0; padding-left: 18px; user-select: text; }
</style>
