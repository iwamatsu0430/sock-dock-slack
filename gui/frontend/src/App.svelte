<script lang="ts">
  import Tray from "./Tray.svelte";
  import Main from "./Main.svelte";
  import { startLive } from "./lib/live.svelte";
  import { startI18n } from "./lib/i18n/index.svelte";

  // Each Wails window loads the same bundle with a different hash.
  const view = location.hash.startsWith("#/tray") ? "tray" : "main";
  startI18n();
  startLive();
</script>

{#if view === "tray"}
  <Tray />
{:else}
  <Main />
{/if}
