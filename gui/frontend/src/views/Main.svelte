<script lang="ts">
  import { Events } from "@wailsio/runtime";
  import { App, SockdockNavigate } from "../lib/api";
  import { live, needsSetup, banner } from "../lib/live.svelte";
  import { t } from "../lib/i18n/index.svelte";
  import Setup from "./Setup.svelte";
  import Rules from "./Rules.svelte";
  import Rule from "./Rule.svelte";

  // The main window: setup until Slack is connected, then the rule list and
  // one rule at a time (two levels, docs/ia.md §4).
  type View = { kind: "list" } | { kind: "rule"; name: string } | { kind: "new" };
  let view = $state<View>({ kind: "list" });
  let connectedTo = $state("");

  Events.On(SockdockNavigate, (ev) => {
    const name = ev.data as string;
    view = name ? { kind: "rule", name } : { kind: "list" };
  });

  const setup = $derived(needsSetup(live.snap));
  const bannerKind = $derived(banner(live.snap, live.now));
</script>

{#if !live.snap}
  <div></div>
{:else if setup}
  <Setup onconnected={(team) => ((connectedTo = team), (view = { kind: "list" }))} />
{:else}
  <div class="content">
    {#if bannerKind}
      <div class="banner">
        <span>{t(bannerKind === "bot" ? "banner.bot" : bannerKind === "app" ? "banner.app" : "banner.retry")}</span>
        <span class="spacer"></span>
        {#if bannerKind !== "retry"}<button class="small" onclick={() => App.OpenSettings()}>{t("banner.reset")}</button>{/if}
      </div>
    {/if}
    {#if view.kind === "list"}
      <Rules {connectedTo} onopen={(name) => ((connectedTo = ""), (view = { kind: "rule", name }))} onadd={() => ((connectedTo = ""), (view = { kind: "new" }))} />
    {:else}
      {#key view}
        <Rule name={view.kind === "rule" ? view.name : ""} onclose={() => (view = { kind: "list" })} />
      {/key}
    {/if}
  </div>
{/if}

<style>
  .content { height: 100%; overflow-y: auto; padding: 16px 28px 24px; }
  .banner { display: flex; gap: 10px; align-items: center; padding: 9px 12px; border-radius: 8px; margin-bottom: 14px;
    background: color-mix(in srgb, var(--bad) 10%, var(--panel)); border: 1px solid color-mix(in srgb, var(--bad) 40%, var(--border)); }
</style>
