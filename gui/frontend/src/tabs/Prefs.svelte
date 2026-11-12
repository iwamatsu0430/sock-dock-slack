<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type AppInfo, type Prefs } from "../lib/api";

  let prefs = $state<Prefs>({ notifications: true });
  let launchAtLogin = $state(false);
  let info = $state<AppInfo | null>(null);
  let error = $state("");

  onMount(async () => {
    prefs = await App.GetPrefs();
    launchAtLogin = await App.LaunchAtLogin();
    info = await App.Info();
  });

  async function setNotifications(on: boolean) {
    prefs = { ...prefs, notifications: on };
    try {
      await App.SetPrefs(prefs);
    } catch (e) {
      error = errorText(e);
    }
  }

  async function setLaunch(on: boolean) {
    error = "";
    try {
      await App.SetLaunchAtLogin(on);
      launchAtLogin = on;
    } catch (e) {
      error = errorText(e);
      launchAtLogin = await App.LaunchAtLogin();
    }
  }
</script>

<div class="stack">
  <h1>Preferences</h1>
  <div class="card stack">
    <label class="row toggle">
      <input type="checkbox" checked={launchAtLogin} onchange={(e) => setLaunch(e.currentTarget.checked)} />
      <span>Open the menu bar app at login</span>
    </label>
    <p class="small muted">The gateway itself starts at login when the background service is installed (Setup), with or without this app.</p>
    <label class="row toggle">
      <input type="checkbox" checked={prefs.notifications} onchange={(e) => setNotifications(e.currentTarget.checked)} />
      <span>Notify me when the gateway is down or a handler fails</span>
    </label>
    {#if error}<div class="error-box">{error}</div>{/if}
  </div>

  {#if info}
    <div class="card stack">
      <h2>About</h2>
      <p>SockDock for Slack <span class="muted">v{info.version}</span></p>
      <p class="small muted">Not affiliated with, created by, or supported by Slack Technologies, LLC.</p>
      <table class="small">
        <tbody>
          <tr><td class="muted">Config</td><td class="mono">{info.config_path}</td></tr>
          <tr><td class="muted">Data</td><td class="mono">{info.data_dir}</td></tr>
          <tr><td class="muted">Logs</td><td class="mono">{info.log_dir}</td></tr>
        </tbody>
      </table>
      <div class="row">
        <button onclick={() => App.OpenURL("https://github.com/iwamatsu0430/sock-dock-slack")}>GitHub</button>
      </div>
    </div>
  {/if}
</div>

<style>
  .toggle { font-weight: normal; margin: 0; gap: 8px; }
  td.mono { user-select: text; word-break: break-all; }
</style>
