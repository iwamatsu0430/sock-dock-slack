<script lang="ts">
  import Main from "./views/Main.svelte";
  import { startLive } from "./lib/live.svelte";
  import { startI18n } from "./lib/i18n/index.svelte";

  // One window; Main switches between the rules and settings views.
  startI18n();
  startLive();
</script>

<Main />
