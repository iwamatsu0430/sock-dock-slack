<script lang="ts">
  import type { Snippet } from "svelte";
  import { rich, type Segment } from "./i18n/index.svelte";
  import type { MessageKey } from "./i18n/en";

  let {
    key,
    params,
    slot,
  }: { key: MessageKey; params?: Record<string, string | number>; slot?: Snippet<[string]> } = $props();

  const segments: Segment[] = $derived(rich(key, params));
</script>

{#each segments as s}{#if s.kind === "code"}<code>{s.text}</code>{:else if s.kind === "em"}<em>{s.text}</em>{:else if s.kind === "slot"}{#if slot}{@render slot(s.text)}{/if}{:else}{s.text}{/if}{/each}
