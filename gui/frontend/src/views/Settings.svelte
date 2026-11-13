<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type Prefs } from "../lib/api";
  import { live, needsSetup } from "../lib/live.svelte";
  import { languageNames, setLocale, t } from "../lib/i18n/index.svelte";
  import TokenFields from "../lib/TokenFields.svelte";

  // The settings window (⌘,): Slack and General (docs/ia.md「設定ウィンドウ」).
  let prefs = $state<Prefs | null>(null);
  let atLogin = $state(false);
  let version = $state("");
  let replacing = $state(false);
  let error = $state("");

  onMount(async () => {
    [prefs, atLogin, version] = await Promise.all([App.GetPrefs(), App.LaunchAtLogin(), App.Version()]);
  });

  const s = $derived(live.snap);
  const setup = $derived(needsSetup(s) || s?.gateway?.state === "stopped");
  const connected = $derived(!!s?.running && !!s.status?.connected);
  const team = $derived(s?.status?.identity?.team ?? "");

  async function savePrefs(p: Prefs) {
    error = "";
    try {
      await App.SetPrefs(p);
      prefs = p;
      setLocale(await App.Locale());
    } catch (e) {
      error = errorText(e);
    }
  }
  async function toggleLogin() {
    try {
      await App.SetLaunchAtLogin(!atLogin);
      atLogin = !atLogin;
    } catch (e) {
      error = errorText(e);
    }
  }
  async function disconnect() {
    if (!confirm(t("settings.disconnectConfirm"))) return;
    try {
      await App.DisconnectSlack();
    } catch (e) {
      error = errorText(e);
    }
  }
</script>

<div class="set">
  <div>
    <h2>{t("settings.slack")}</h2>
    <div class="card">
      <div class="item">
        {#if setup}
          <span class="lbl muted">{t("settings.notConnected")}</span>
          <button class="small" onclick={() => App.OpenMain("")}>{t("settings.connect")}</button>
        {:else}
          <span class="lbl">
            {#if connected}<span class="ok-t">●</span> {t("settings.connectedTo", { team })}
            {:else}<span class="bad-t">●</span> {t("settings.cannotConnect")}{/if}
          </span>
          <button class="small" onclick={() => (replacing = !replacing)}>{t("settings.reset")}</button>
          <button class="small danger" onclick={disconnect}>{t("settings.disconnect")}</button>
        {/if}
      </div>
      {#if replacing && !setup}
        <div class="item stack replace">
          <TokenFields backLabel={t("settings.cancel")} onback={() => (replacing = false)} onconnected={() => (replacing = false)} />
        </div>
      {/if}
    </div>
  </div>

  <div>
    <h2>{t("settings.general")}</h2>
    <div class="card">
      <div class="item"><span class="lbl">{t("settings.login")}</span>
        <button class="switch" class:on={atLogin} aria-label={t("settings.login")} aria-pressed={atLogin} onclick={toggleLogin}></button></div>
      <div class="item"><span class="lbl">{t("settings.notify")}</span>
        <button class="switch" class:on={prefs?.notifications} aria-label={t("settings.notify")} aria-pressed={!!prefs?.notifications}
          disabled={!prefs} onclick={() => prefs && savePrefs({ ...prefs, notifications: !prefs.notifications })}></button></div>
      <div class="item"><span class="lbl">{t("settings.language")}</span>
        <select class="lang" value={prefs?.language ?? "auto"} disabled={!prefs}
          onchange={(e) => prefs && savePrefs({ ...prefs, language: (e.currentTarget as HTMLSelectElement).value })}>
          <option value="auto">{t("settings.languageAuto")}</option>
          {#each Object.entries(languageNames) as [code, label]}<option value={code}>{label}</option>{/each}
        </select></div>
    </div>
  </div>

  {#if error}<p class="small bad-t">{error}</p>{/if}
  <p class="small muted">SockDock {version} · <button class="link small" onclick={() => App.ShowLogs()}>{t("settings.showLogs")}</button></p>
</div>

<style>
  .set { padding: 18px 24px 22px; display: flex; flex-direction: column; gap: 18px; }
  .set h2 { color: var(--muted); font-weight: 500; margin-bottom: 6px; }
  .item { display: flex; align-items: center; gap: 10px; padding: 10px 14px; }
  .item + .item { border-top: 1px solid var(--border); }
  .lbl { flex: 1; }
  .replace { align-items: stretch; gap: 8px; }
  .lang { width: 180px; }
</style>
