<script lang="ts">
  import { Events } from "@wailsio/runtime";
  import { SockdockNavigate } from "../lib/api";
  import { live, needsSetup, banner, claudeSignedOut, signInClaude } from "../lib/live.svelte";
  import { errorText } from "../lib/api";
  import { t } from "../lib/i18n/index.svelte";
  import Setup from "./Setup.svelte";
  import Rules from "./Rules.svelte";
  import Rule from "./Rule.svelte";
  import Settings from "./Settings.svelte";

  // The one window: setup until Slack is connected, then [Rules | Settings].
  // Rules has two levels: the list and one rule (docs/ia.md §4).
  type View = { kind: "list" } | { kind: "rule"; name: string } | { kind: "new" } | { kind: "settings" };
  let view = $state<View>({ kind: "list" });
  let connectedTo = $state("");

  // ":settings" opens settings (⌘, and the menu's Settings…); rule names
  // never start with ':'.
  Events.On(SockdockNavigate, (ev) => {
    const name = ev.data as string;
    view = name === ":settings" ? { kind: "settings" } : name ? { kind: "rule", name } : { kind: "list" };
  });

  // Setup stays until 「Slack に接続」 succeeds (SockDock really connected), not
  // just until tokens exist.
  let inSetup = $state(false);
  $effect(() => {
    if (needsSetup(live.snap)) inSetup = true;
  });
  const setup = $derived(inSetup || needsSetup(live.snap));
  const bannerKind = $derived(banner(live.snap, live.now));
  let signInError = $state("");
  async function signIn() {
    signInError = "";
    try {
      await signInClaude();
    } catch (e) {
      signInError = errorText(e);
    }
  }
  const tab = $derived(view.kind === "settings" ? "settings" : "rules");
</script>

{#if !live.snap}
  <div></div>
{:else if setup}
  <div class="scroll">
    <Setup onconnected={(team) => ((inSetup = false), (connectedTo = team), (view = { kind: "list" }))} />
  </div>
{:else}
  <div class="frame">
    <div class="topnav">
      <div class="seg" role="tablist">
        <button role="tab" aria-selected={tab === "rules"} class:on={tab === "rules"} onclick={() => (view = { kind: "list" })}>{t("nav.rules")}</button>
        <button role="tab" aria-selected={tab === "settings"} class:on={tab === "settings"} onclick={() => ((connectedTo = ""), (view = { kind: "settings" }))}>{t("nav.settings")}</button>
      </div>
    </div>
    <div class="scroll content">
      {#if bannerKind}
        <div class="banner">
          <span>{t(bannerKind === "bot" ? "banner.bot" : bannerKind === "app" ? "banner.app" : "banner.retry")}</span>
          <span class="spacer"></span>
          {#if bannerKind !== "retry" && view.kind !== "settings"}<button class="small" onclick={() => (view = { kind: "settings" })}>{t("banner.reset")}</button>{/if}
        </div>
      {/if}
      {#if view.kind === "list" && claudeSignedOut(live.snap)}
        <div class="banner">
          <span>{t("banner.claude")}{#if signInError}<br /><span class="small">{signInError}</span>{/if}</span>
          <span class="spacer"></span>
          <button class="small" onclick={signIn}>{t("claude.signIn")}</button>
        </div>
      {/if}
      {#if view.kind === "settings"}
        <Settings />
      {:else if view.kind === "list"}
        <Rules {connectedTo} onopen={(name) => ((connectedTo = ""), (view = { kind: "rule", name }))} onadd={() => ((connectedTo = ""), (view = { kind: "new" }))} />
      {:else}
        {#key view}
          <Rule name={view.kind === "rule" ? view.name : ""} onclose={() => (view = { kind: "list" })} />
        {/key}
      {/if}
    </div>
  </div>
{/if}

<style>
  .frame { height: 100%; display: flex; flex-direction: column; }
  .topnav { flex: none; display: flex; justify-content: center; padding: 10px 0 8px; }
  .seg { display: inline-flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
  .seg button { border: none; border-radius: 0; padding: 3px 18px; background: var(--panel); }
  .seg button + button { border-left: 1px solid var(--border); }
  .seg button.on, .seg button.on:hover { background: var(--accent); color: var(--accent-text); }
  .scroll { flex: 1; min-height: 0; height: 100%; overflow-y: auto; }
  .content { padding: 8px 28px 24px; }
  .banner { display: flex; gap: 10px; align-items: center; padding: 9px 12px; border-radius: 8px; margin-bottom: 14px;
    background: color-mix(in srgb, var(--bad) 10%, var(--panel)); border: 1px solid color-mix(in srgb, var(--bad) 40%, var(--border)); }
</style>
