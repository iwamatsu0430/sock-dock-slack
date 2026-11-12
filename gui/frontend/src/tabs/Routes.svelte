<script lang="ts">
  import { App, errorText, type MatchResult } from "../lib/api";
  import { live } from "../lib/live.svelte";

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
  <h1>Routes</h1>
  <p class="muted">
    A route starts a thread when a top-level message in its channel matches its regular expression. Every reply in that thread then goes to the same
    handler. Named groups like <code>(?P&lt;code&gt;\w+)</code> are passed to the handler.
  </p>

  <div class="card stack">
    <h2>Configured routes</h2>
    {#if (live.snap?.status?.routes ?? []).length === 0}
      <p class="small muted">{live.snap?.running ? "No routes yet." : "Start the gateway to see the active routes, or edit them in Config."}</p>
    {:else}
      <table>
        <thead><tr><th>Name</th><th>Type</th><th>Channel</th><th>Match</th><th>State</th></tr></thead>
        <tbody>
          {#each live.snap!.status!.routes as r (r.name)}
            <tr>
              <td><strong>{r.name}</strong></td>
              <td>{r.type}</td>
              <td class="mono">{r.channel}</td>
              <td class="mono">{r.match}</td>
              <td>
                <button class="small" onclick={() => App.SetPaused(r.name, !r.paused)}>{r.paused ? "Paused — resume" : "Active — pause"}</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  <div class="card stack">
    <h2>Test a message</h2>
    <p class="small muted">Checks the saved config: which route would a new top-level message start?</p>
    <div class="row">
      <input type="text" placeholder="Channel ID (C…)" bind:value={testChannel} style="max-width: 180px" />
      <input type="text" placeholder="Message text" bind:value={testText} onkeydown={(e) => e.key === "Enter" && test()} />
      <button onclick={test}>Test</button>
    </div>
    {#if testError}<div class="error-box">{testError}</div>{/if}
    {#if result}
      {#if result.matched}
        <div class="ok-box">
          Route <strong>{result.route}</strong>
          {#each Object.entries(result.groups ?? {}) as [k, v]}
            <span class="small"> · {k} = <code>{v}</code></span>
          {/each}
        </div>
      {:else}
        <p class="muted">No route matches.</p>
      {/if}
    {/if}
  </div>

  <div class="card stack">
    <h2>Add a route</h2>
    <div class="grid2">
      <div><label for="r-name">Name</label><input id="r-name" type="text" bind:value={name} placeholder="web-app-entry" /></div>
      <div><label for="r-ch">Channel ID</label><input id="r-ch" type="text" bind:value={channel} placeholder="C0123456789 or ${'{'}VAR}" /></div>
    </div>
    <div>
      <label for="r-match">Match (regular expression on the first message)</label>
      <input id="r-match" type="text" bind:value={match} placeholder={"^\\s*entry\\s+(?P<code>\\w+)"} />
      {#if regexError}<p class="small" style="color: var(--bad)">{regexError}</p>{/if}
    </div>
    <div class="grid2">
      <div>
        <label for="r-type">Handler</label>
        <select id="r-type" bind:value={type}>
          <option value="claude">Claude Code (a conversation per thread)</option>
          <option value="command">Command (event JSON on stdin, stdout is the reply)</option>
        </select>
      </div>
      <div><label for="r-cwd">Working directory</label><input id="r-cwd" type="text" bind:value={cwd} placeholder="~/projects/my-project" /></div>
    </div>
    {#if type === "command"}
      <div><label for="r-cmd">Command</label><input id="r-cmd" type="text" bind:value={command} placeholder="./handler.sh --flag" /></div>
    {:else}
      <div class="grid2">
        <div><label for="r-prompt">Prompt file (relative to the working directory)</label><input id="r-prompt" type="text" bind:value={prompt} placeholder="prompts/slack.md" /></div>
        <div><label for="r-model">Model (optional)</label><input id="r-model" type="text" bind:value={model} placeholder="opus" /></div>
      </div>
      <div>
        <label for="r-tools">Allowed tools (one rule per line)</label>
        <textarea id="r-tools" class="code" rows="3" bind:value={allowedTools} placeholder={"Bash(make test *)\nWebSearch"}></textarea>
        <p class="small muted">Only these tools run without approval; everything else is denied. Read, Glob and Grep are always available.</p>
      </div>
    {/if}
    <div style="max-width: 160px"><label for="r-timeout">Timeout (seconds)</label><input id="r-timeout" type="number" min="10" bind:value={timeout} /></div>

    <div>
      <label for="r-preview">YAML</label>
      <pre id="r-preview" class="preview">{snippet}</pre>
    </div>
    <div class="row">
      <button class="primary" disabled={!name || !channel || !match || !cwd || !!regexError} onclick={() => onaddroute(snippet)}>Add to config…</button>
      <span class="small muted">Opens the config editor with this route appended. Review, then save.</span>
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
