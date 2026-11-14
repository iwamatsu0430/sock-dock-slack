<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type MCPSetup, type OwnerState, type Prefs } from "../lib/api";
  import { live, needsSetup } from "../lib/live.svelte";
  import { languageNames, setLocale, t } from "../lib/i18n/index.svelte";
  import TokenFields from "../lib/TokenFields.svelte";
  import Rich from "../lib/Rich.svelte";

  // The settings view of the window (⌘,): Slack, General, Notifications, MCP
  // and reset (docs/ia.md「設定」).
  let prefs = $state<Prefs | null>(null);
  let atLogin = $state(false);
  let version = $state("");
  let replacing = $state(false);
  let error = $state("");
  let mcp = $state<MCPSetup | null>(null);
  let copied = $state(false);
  let confirmReset = $state(false);
  let resetting = $state(false);

  // MCP clients, in the order of docs/mcp.md §2.1.
  const mcpClients: [string, string][] = [
    ["claude-code", "Claude Code"], ["codex", "Codex"], ["gemini", "Gemini CLI"], ["vscode", "VS Code"],
    ["claude-desktop", "Claude Desktop"], ["cursor", "Cursor"], ["other", ""],
  ];
  const clientName = (id: string) => mcpClients.find(([k]) => k === id)?.[1] || t("settings.mcpOther");

  onMount(async () => {
    [prefs, atLogin, version, mcp] = await Promise.all([App.GetPrefs(), App.LaunchAtLogin(), App.Version(), App.MCPSetup("")]);
  });

  async function pickClient(id: string) {
    copied = false;
    mcp = await App.MCPSetup(id);
  }
  async function copyMCP() {
    if (!mcp) return;
    if (mcp.kind === "extension") {
      error = "";
      try {
        await App.InstallClaudeDesktopExtension();
      } catch (e) {
        error = errorText(e);
      }
      return;
    }
    try {
      await App.CopyMCPSetup(mcp.client);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch (e) {
      error = errorText(e);
    }
  }

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
  async function reset() {
    resetting = true;
    error = "";
    try {
      await App.ResetAll();
      confirmReset = false;
    } catch (e) {
      error = errorText(e);
      confirmReset = false;
    } finally {
      resetting = false;
    }
  }
  // The owner ("本人", docs/ia.md「本人」): registered with a code sent by DM.
  let owner = $state<OwnerState | null>(null);
  async function loadOwner() {
    owner = await App.GetOwner().catch(() => null);
  }
  $effect(() => {
    live.ownerChanges;
    if (!setup) loadOwner();
    else owner = null;
  });
  // The code changes every 30 seconds: fetch the next one when it does.
  $effect(() => {
    const next = owner?.registration?.next_change;
    if (!owner?.registration?.active || !next) return;
    const timer = setTimeout(loadOwner, Math.max(new Date(next).getTime() - Date.now() + 200, 500));
    return () => clearTimeout(timer);
  });
  const reg = $derived(owner?.registration?.active ? owner.registration : null);
  const codeLeft = $derived(
    reg?.next_change ? Math.max(0, Math.min(1, (new Date(reg.next_change).getTime() - live.now) / ((reg.step_sec || 30) * 1000))) : 0,
  );
  // The code can be pasted into the DM instead of typed.
  let codeCopied = $state(false);
  async function copyCode() {
    if (!reg?.code) return;
    try {
      await App.CopyText(reg.code);
      codeCopied = true;
      setTimeout(() => (codeCopied = false), 2000);
    } catch (e) {
      error = errorText(e);
    }
  }
  async function ownerAction(f: () => Promise<OwnerState>) {
    error = "";
    try {
      const o = await f();
      if (o.error) error = o.error;
      else owner = o;
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
      {#if !setup && owner && !owner.error}
        <div class="item owner">
          {#if owner.owner}
            <span class="olbl">{t("settings.owner")}</span>
            <span class="lbl">{owner.owner.name || owner.owner.id}</span>
            <button class="small" onclick={() => ownerAction(App.ClearOwner)}>{t("settings.ownerRemove")}</button>
          {:else if owner.problem === "missing_scope"}
            <span class="lbl small bad-t"><Rich key="settings.ownerNoScope" /></span>
          {:else if reg}
            <div class="lbl ostack">
              <div class="code"><b>{(reg.code ?? "").slice(0, 3)} {(reg.code ?? "").slice(3)}</b><span class="ring" style="--left: {codeLeft * 100}%"></span>
                <button class="small" onclick={copyCode}>{codeCopied ? t("settings.mcpCopied") : t("settings.ownerCopy")}</button></div>
              <span class="small">{t("settings.ownerCode")}</span>
              <div><button class="small" onclick={() => ownerAction(App.CancelOwnerRegistration)}>{t("settings.ownerCancel")}</button></div>
            </div>
          {:else}
            <span class="lbl small">{t("settings.ownerIntro")}</span>
            <button class="small" onclick={() => ownerAction(App.StartOwnerRegistration)}>{t("settings.ownerRegister")}</button>
          {/if}
        </div>
      {/if}
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
      <div class="item"><span class="lbl">{t("settings.checkUpdates")}</span>
        <button class="switch" class:on={s?.status?.check_updates ?? true} aria-label={t("settings.checkUpdates")} aria-pressed={s?.status?.check_updates ?? true}
          disabled={!s?.status} onclick={() => ownerAction(() => App.SetNotifyDM("check_updates", !(s?.status?.check_updates ?? true)))}></button></div>
      <div class="item"><span class="lbl">{t("settings.language")}</span>
        <select class="lang" value={prefs?.language ?? "auto"} disabled={!prefs}
          onchange={(e) => prefs && savePrefs({ ...prefs, language: (e.currentTarget as HTMLSelectElement).value })}>
          <option value="auto">{t("settings.languageAuto")}</option>
          {#each Object.entries(languageNames) as [code, label]}<option value={code}>{label}</option>{/each}
        </select></div>
    </div>
  </div>

  <div>
    <h2>{t("settings.notifications")}</h2>
    <div class="card">
      <div class="item"><span class="lbl">{t("settings.notifyMac")}</span>
        <button class="switch" class:on={prefs?.notifications} aria-label={t("settings.notifyMac")} aria-pressed={!!prefs?.notifications}
          disabled={!prefs} onclick={() => prefs && savePrefs({ ...prefs, notifications: !prefs.notifications })}></button></div>
      <div class="item"><span class="lbl">{t("settings.notifyDM")}
          {#if !owner?.owner}<span class="sub small muted">{t("settings.notifyDMNeedsOwner")}</span>{/if}</span>
        <button class="switch" class:on={!!owner?.owner && owner.notify_dm} aria-label={t("settings.notifyDM")} aria-pressed={!!owner?.owner && owner.notify_dm}
          disabled={!owner?.owner} onclick={() => owner && ownerAction(() => App.SetNotifyDM("notify_dm", !owner!.notify_dm))}></button></div>
      {#if owner?.owner && owner.notify_dm}
        <div class="item indent"><span class="lbl">{t("settings.notifyFailures")}
            <span class="sub small muted">{t("settings.notifyFailuresNote")}</span></span>
          <button class="switch" class:on={owner.notify_dm_failures} aria-label={t("settings.notifyFailures")} aria-pressed={owner.notify_dm_failures}
            onclick={() => owner && ownerAction(() => App.SetNotifyDM("notify_dm_failures", !owner!.notify_dm_failures))}></button></div>
      {/if}
    </div>
  </div>

  <div>
    <h2>{t("settings.mcp")}</h2>
    <div class="card">
      <div class="item stack mcp">
        <div class="row">
          <select class="client" value={mcp?.client ?? "claude-code"} disabled={!mcp}
            onchange={(e) => pickClient((e.currentTarget as HTMLSelectElement).value)}>
            {#each mcpClients as [id] (id)}<option value={id}>{clientName(id)}</option>{/each}
          </select>
          <span class="spacer"></span>
          <button class="small" disabled={!mcp} onclick={copyMCP}>
            {mcp?.kind === "extension" ? t("settings.mcpAddDesktop") : copied ? t("settings.mcpCopied") : mcp?.kind === "config" ? t("settings.mcpCopyConfig") : t("settings.mcpCopyCommand")}
          </button>
        </div>
        {#if mcp}
          <p class="small muted">
            {#if mcp.kind === "extension"}{t("settings.mcpDesktopHelp")}
            {:else if mcp.kind === "command"}{t("settings.mcpCommandHelp", { client: clientName(mcp.client) })}
            {:else if mcp.file}<Rich key="settings.mcpConfigHelp" params={{ file: mcp.file, client: clientName(mcp.client) }} />
            {:else}{t("settings.mcpOtherHelp")}{/if}
          </p>
        {/if}
      </div>
    </div>
  </div>

  <div><button class="small danger" onclick={() => (confirmReset = true)}>{t("settings.resetAll")}</button></div>

  {#if error}<p class="small bad-t">{error}</p>{/if}
  <p class="small muted">SockDock {version} · <button class="link small" onclick={() => App.ShowLogs()}>{t("settings.showLogs")}</button></p>
</div>

{#if confirmReset}
  <div class="overlay" role="presentation">
    <div class="dialog" role="alertdialog" aria-labelledby="reset-title">
      <p class="big" id="reset-title">{t("reset.title")}</p>
      <p>{t("reset.text")}</p>
      <div class="row"><span class="spacer"></span>
        <button disabled={resetting} onclick={() => (confirmReset = false)}>{t("reset.cancel")}</button>
        <button class="danger-fill" disabled={resetting} onclick={reset}>{t("reset.confirm")}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .set { max-width: 560px; margin: 4px auto 0; display: flex; flex-direction: column; gap: 18px; }
  .overlay { position: fixed; inset: 0; background: rgba(0, 0, 0, .28); display: flex; align-items: flex-start; justify-content: center; padding-top: 80px; z-index: 10; }
  .dialog { width: 380px; background: var(--panel); border: 1px solid var(--border); border-radius: 10px; padding: 18px 20px;
    display: flex; flex-direction: column; gap: 10px; box-shadow: 0 16px 40px rgba(0, 0, 0, .3); }
  .dialog .big { font-size: 15px; font-weight: 600; }
  .danger-fill, .danger-fill:hover:not(:disabled) { background: var(--bad); border-color: var(--bad); color: #fff; }
  .set h2 { color: var(--muted); font-weight: 500; margin-bottom: 6px; }
  .item { display: flex; align-items: center; gap: 10px; padding: 10px 14px; }
  .item + .item { border-top: 1px solid var(--border); }
  .lbl { flex: 1; }
  .replace { align-items: stretch; gap: 8px; }
  .lang { width: 180px; }
  .mcp { align-items: stretch; gap: 6px; }
  .client { width: auto; }
  .owner { align-items: center; }
  .olbl { width: 44px; flex: none; }
  .ostack { display: flex; flex-direction: column; gap: 4px; align-items: flex-start; }
  .code { display: flex; align-items: center; gap: 10px; }
  .code b { font: 600 26px/1.2 ui-monospace, Menlo, monospace; letter-spacing: .06em; }
  .ring { width: 16px; height: 16px; border-radius: 50%; flex: none; background: conic-gradient(var(--accent) var(--left, 0%), var(--border) 0); }
  .sub { display: block; }
  .indent { padding-left: 28px; }
</style>
