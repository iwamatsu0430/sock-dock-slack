<script lang="ts">
  import Tray from "./Tray.svelte";
  import Main from "./Main.svelte";
  import { startLive } from "./lib/live.svelte";

  // Each Wails window loads the same bundle with a different hash.
  const view = location.hash.startsWith("#/tray") ? "tray" : "main";
  startLive();
</script>

{#if view === "tray"}
  <Tray />
{:else}
  <Main />
{/if}
