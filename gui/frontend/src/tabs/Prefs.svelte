<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type AppInfo, type Prefs } from "../lib/api";
  import { t, setLocale, languageNames } from "../lib/i18n/index.svelte";
  import Rich from "../lib/Rich.svelte";

  let prefs = $state<Prefs>({ notifications: true, language: "auto" });
  let languages = $state<string[]>([]);
  let systemLang = $state("");
  let launchAtLogin = $state(false);
  let info = $state<AppInfo | null>(null);
  let error = $state("");

  onMount(async () => {
    prefs = await App.GetPrefs();
    languages = (await App.Languages()) ?? [];
    launchAtLogin = await App.LaunchAtLogin();
    info = await App.Info();
    // What "auto" currently resolves to.
    if (prefs.language === "auto") systemLang = await App.Locale();
  });

  async function save(next: Prefs) {
    error = "";
    const before = prefs;
    prefs = next;
    try {
      await App.SetPrefs(next);
      const resolved = await App.Locale();
      if (next.language === "auto") systemLang = resolved;
      setLocale(resolved);
    } catch (e) {
      error = errorText(e);
      prefs = before;
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
  <h1>{t("tab.prefs")}</h1>
  <div class="card stack">
    <div class="lang">
      <label for="p-lang">{t("prefs.language")}</label>
      <select id="p-lang" value={prefs.language} onchange={(e) => save({ ...prefs, language: e.currentTarget.value })}>
        <option value="auto">{t("prefs.languageAuto", { lang: languageNames[systemLang] ?? systemLang })}</option>
        {#each languages as l}
          <option value={l}>{languageNames[l] ?? l}</option>
        {/each}
      </select>
      <p class="small muted"><Rich key="prefs.languageHelp" /></p>
    </div>
    <label class="row toggle">
      <input type="checkbox" checked={launchAtLogin} onchange={(e) => setLaunch(e.currentTarget.checked)} />
      <span>{t("prefs.launchAtLogin")}</span>
    </label>
    <p class="small muted">{t("prefs.launchHelp")}</p>
    <label class="row toggle">
      <input type="checkbox" checked={prefs.notifications} onchange={(e) => save({ ...prefs, notifications: e.currentTarget.checked })} />
      <span>{t("prefs.notifications")}</span>
    </label>
    {#if error}<div class="error-box">{error}</div>{/if}
  </div>

  {#if info}
    <div class="card stack">
      <h2>{t("prefs.about")}</h2>
      <p>SockDock for Slack <span class="muted">v{info.version}</span></p>
      <p class="small muted">{t("prefs.notAffiliated")}</p>
      <table class="small">
        <tbody>
          <tr><td class="muted">{t("prefs.config")}</td><td class="mono">{info.config_path}</td></tr>
          <tr><td class="muted">{t("prefs.data")}</td><td class="mono">{info.data_dir}</td></tr>
          <tr><td class="muted">{t("prefs.logs")}</td><td class="mono">{info.log_dir}</td></tr>
        </tbody>
      </table>
      <div class="row">
        <button onclick={() => App.OpenURL("https://github.com/iwamatsu0430/sock-dock-slack")}>GitHub</button>
      </div>
    </div>
  {/if}
</div>

<style>
  .lang { max-width: 320px; }
  .toggle { font-weight: normal; margin: 0; gap: 8px; }
  td.mono { user-select: text; word-break: break-all; }
</style>
