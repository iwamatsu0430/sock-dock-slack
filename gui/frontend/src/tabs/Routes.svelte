<script lang="ts">
  import { App, errorText, type MatchResult } from "../lib/api";
  import { live } from "../lib/live.svelte";
  import { t } from "../lib/i18n/index.svelte";
  import Rich from "../lib/Rich.svelte";

  let { onaddroute }: { onaddroute: (snippet: string) => void } = $props();

  // ---- tester ----
  let testChannel = $state("");
  let testText = $state("");
  let result = $state<MatchResult | null>(null);
  let testError = $state("");

  async function test() {
    testError = "";
    result = null;
    try {
      const cfg = await App.GetConfig();
      result = await App.MatchInContent(cfg.content, testChannel.trim(), testText);
    } catch (e) {
      testError = errorText(e);
    }
  }

  // ---- new route form ----
  let name = $state("");
  let channel = $state("");
  let match = $state("");
  let type = $state<"command" | "claude">("claude");
  let cwd = $state("");
  let command = $state("");
  let prompt = $state("");
  let allowedTools = $state("");
  let model = $state("");
  let timeout = $state(600);

  const q = (s: string) => JSON.stringify(s); // JSON strings are valid YAML
  const sq = (s: string) => `'${s.replaceAll("'", "''")}'`; // keeps regex backslashes literal

  const snippet = $derived.by(() => {
    const lines = [`  - name: ${q(name || "my-route")}`, `    channel: ${q(channel || "C0123456789")}`, `    match: ${sq(match || "^hello")}`];
    lines.push("    handler:", `      type: ${type}`, `      cwd: ${q(cwd || "~/")}`);
    if (type === "command") {
      const argv = command.trim() ? command.trim().split(/\s+/) : ["./handler.sh"];
      lines.push(`      command: [${argv.map(q).join(", ")}]`);
    } else {
      if (prompt.trim()) lines.push(`      prompt: ${q(prompt.trim())}`);
      const tools = allowedTools
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean);
      if (tools.length) lines.push(`      allowed_tools: [${tools.map(q).join(", ")}]`);
      if (model.trim()) lines.push(`      model: ${q(model.trim())}`);
    }
    lines.push(`      timeout_sec: ${timeout || 600}`);
    return lines.join("\n");
  });

  let regexError = $derived.by(() => {
    if (!match) return "";
    try {
      // Go's RE2 differs slightly from JS, but this catches most typos.
      new RegExp(match.replaceAll("(?P<", "(?<"));
      return "";
    } catch (e) {
      return errorText(e);
    }
  });
</script>

<div class="stack">
  <h1>{t("tab.routes")}</h1>
  <p class="muted"><Rich key="routes.intro" /></p>

  <div class="card stack">
    <h2>{t("routes.configured")}</h2>
    {#if (live.snap?.status?.routes ?? []).length === 0}
      <p class="small muted">{live.snap?.running ? t("routes.none") : t("routes.startToSee")}</p>
    {:else}
      <table>
        <thead><tr><th>{t("col.name")}</th><th>{t("col.type")}</th><th>{t("col.channel")}</th><th>{t("col.match")}</th><th>{t("col.state")}</th></tr></thead>
        <tbody>
          {#each live.snap!.status!.routes as r (r.name)}
            <tr>
              <td><strong>{r.name}</strong></td>
              <td>{r.type}</td>
              <td class="mono">{r.channel}</td>
              <td class="mono">{r.match}</td>
              <td>
                <button class="small" onclick={() => App.SetPaused(r.name, !r.paused)}>{r.paused ? t("routes.pausedResume") : t("routes.activePause")}</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  <div class="card stack">
    <h2>{t("routes.testTitle")}</h2>
    <p class="small muted">{t("routes.testHelp")}</p>
    <div class="row">
      <input type="text" placeholder={t("routes.channelPlaceholder")} bind:value={testChannel} style="max-width: 180px" />
      <input type="text" placeholder={t("routes.textPlaceholder")} bind:value={testText} onkeydown={(e) => e.key === "Enter" && test()} />
      <button onclick={test}>{t("routes.test")}</button>
    </div>
    {#if testError}<div class="error-box">{testError}</div>{/if}
    {#if result}
      {#if result.matched}
        <div class="ok-box">
          {t("routes.matchedRoute")} <strong>{result.route}</strong>
          {#each Object.entries(result.groups ?? {}) as [k, v]}
            <span class="small"> · {k} = <code>{v}</code></span>
          {/each}
        </div>
      {:else}
        <p class="muted">{t("routes.noMatch")}</p>
      {/if}
    {/if}
  </div>

  <div class="card stack">
    <h2>{t("routes.addTitle")}</h2>
    <div class="grid2">
      <div><label for="r-name">{t("routes.name")}</label><input id="r-name" type="text" bind:value={name} placeholder="web-app-entry" /></div>
      <div><label for="r-ch">{t("routes.channelId")}</label><input id="r-ch" type="text" bind:value={channel} placeholder="C0123456789 or ${'{'}VAR}" /></div>
    </div>
    <div>
      <label for="r-match">{t("routes.matchLabel")}</label>
      <input id="r-match" type="text" bind:value={match} placeholder={"^\\s*entry\\s+(?P<code>\\w+)"} />
      {#if regexError}<p class="small" style="color: var(--bad)">{regexError}</p>{/if}
    </div>
    <div class="grid2">
      <div>
        <label for="r-type">{t("routes.handler")}</label>
        <select id="r-type" bind:value={type}>
          <option value="claude">{t("routes.handlerClaude")}</option>
          <option value="command">{t("routes.handlerCommand")}</option>
        </select>
      </div>
      <div><label for="r-cwd">{t("routes.cwd")}</label><input id="r-cwd" type="text" bind:value={cwd} placeholder="~/projects/my-project" /></div>
    </div>
    {#if type === "command"}
      <div><label for="r-cmd">{t("routes.command")}</label><input id="r-cmd" type="text" bind:value={command} placeholder="./handler.sh --flag" /></div>
    {:else}
      <div class="grid2">
        <div><label for="r-prompt">{t("routes.prompt")}</label><input id="r-prompt" type="text" bind:value={prompt} placeholder="prompts/slack.md" /></div>
        <div><label for="r-model">{t("routes.model")}</label><input id="r-model" type="text" bind:value={model} placeholder="opus" /></div>
      </div>
      <div>
        <label for="r-tools">{t("routes.tools")}</label>
        <textarea id="r-tools" class="code" rows="3" bind:value={allowedTools} placeholder={"Bash(make test *)\nWebSearch"}></textarea>
        <p class="small muted">{t("routes.toolsHelp")}</p>
      </div>
    {/if}
    <div style="max-width: 160px"><label for="r-timeout">{t("routes.timeout")}</label><input id="r-timeout" type="number" min="10" bind:value={timeout} /></div>

    <div>
      <label for="r-preview">{t("routes.yaml")}</label>
      <pre id="r-preview" class="preview">{snippet}</pre>
    </div>
    <div class="row">
      <button class="primary" disabled={!name || !channel || !match || !cwd || !!regexError} onclick={() => onaddroute(snippet)}>{t("routes.addToConfig")}</button>
      <span class="small muted">{t("routes.addHelp")}</span>
    </div>
  </div>
</div>

<style>
  .preview {
    margin: 0;
    padding: 10px 12px;
    background: var(--code-bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow-x: auto;
    user-select: text;
  }
</style>
