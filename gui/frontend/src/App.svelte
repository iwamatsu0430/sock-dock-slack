<script lang="ts">
  import Main from "./views/Main.svelte";
  import Settings from "./views/Settings.svelte";
  import { startLive } from "./lib/live.svelte";
  import { startI18n } from "./lib/i18n/index.svelte";

  // Each Wails window loads the same bundle with a different hash.
  const view = location.hash.startsWith("#/settings") ? "settings" : "main";
  startI18n();
  startLive();
</script>

{#if view === "settings"}
  <Settings />
{:else}
  <Main />
{/if}
