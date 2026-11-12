<script lang="ts">
  import { untrack } from "svelte";
  import { App, errorText, type Channel, type ChannelList, type FieldProblem, type RouteForm } from "./api";
  import { t } from "./i18n/index.svelte";
  import type { MessageKey } from "./i18n/en";
  import Rich from "./Rich.svelte";
  import { live } from "./live.svelte";

  let {
    initial,
    channels,
    onreloadchannels,
    onsaved,
    oncancel,
  }: {
    initial: RouteForm;
    channels: ChannelList | null;
    onreloadchannels: () => void;
    onsaved: (message: string) => void;
    oncancel: () => void;
  } = $props();

  // The editor starts from a copy of `initial` (later changes to it are ignored).
  const start = untrack(() => structuredClone($state.snapshot(initial)) as RouteForm);
  let f = $state<RouteForm>(start);
  let commandsText = $state((start.commands ?? []).join("\n"));
  let otherText = $state((start.other_tools ?? []).join("\n"));
  let nameTouched = $state(!!start.original_name);
  let problems = $state<FieldProblem[]>([]);
  let configProblems = $state<string[]>([]);
  let error = $state("");
  let saving = $state(false);
  // After the first save attempt, re-check as the user types so stale errors go away.
  let attempted = $state(false);
  let vtimer: ReturnType<typeof setTimeout> | undefined;
  const lines = (s: string) => s.split("\n").map((x) => x.trim()).filter(Boolean);
  const payload = (): RouteForm => ({ ...$state.snapshot(f), commands: lines(commandsText), other_tools: lines(otherText) }) as RouteForm;
  $effect(() => {
    JSON.stringify(f);
    commandsText;
    otherText;
    if (!attempted) return;
    clearTimeout(vtimer);
    vtimer = setTimeout(async () => {
      problems = (await App.ValidateRoute(payload())) ?? [];
    }, 300);
  });

  const isNew = !start.original_name;
  const list: Channel[] = $derived(channels?.channels ?? []);
  const canPick = $derived(list.length > 0);
  // Show the paste box when the list is unavailable or the value is not in it.
  let pasteMode = $state(false);
  $effect(() => {
    if (channels && !canPick) pasteMode = true;
    if (canPick && f.channel && !list.some((c) => c.id === f.channel)) pasteMode = true;
  });

  // Suggest a name from the keyword until the user edits the name.
  $effect(() => {
    if (!nameTouched && f.trigger === "keyword") {
      f.name = (f.keyword || "").toLowerCase().replace(/[^a-z0-9_.-]+/g, "-").replace(/^-+|-+$/g, "");
    }
  });

  function problem(field: string): string {
    const p = problems.find((x) => x.field === field);
    if (!p) return "";
    return t(("err." + p.code) as MessageKey, { detail: p.detail ?? "" });
  }

  async function pickFolder() {
    const p = await App.PickFolder(t(f.type === "claude" ? "form.folder" : "form.folderCommand"));
    if (p) f.folder = p;
  }

  async function pickProgram() {
    const p = await App.PickFile(t("form.program"));
    if (!p) return;
    f.program = p;
    if (!f.folder) f.folder = p.replace(/\/[^/]+$/, "") || "~";
  }

  async function save() {
    saving = true;
    attempted = true;
    error = "";
    configProblems = [];
    try {
      const res = await App.SaveRoute(payload());
      problems = res.problems ?? [];
      configProblems = res.config_problems ?? [];
      if (res.ok) {
        const parts = [t("form.saved"), live.snap?.running ? t("form.savedLive") : t("form.savedNotRunning")];
        if (res.claude_command) parts.push(t("form.claudeFound", { path: res.claude_command }));
        onsaved(parts.join(" "));
      }
    } catch (e) {
      error = errorText(e);
    } finally {
      saving = false;
    }
  }
</script>

<div class="card stack editor">
  <h2>{isNew ? t("form.newTitle") : t("form.editTitle")}</h2>

  <!-- 1. Where -->
  <section class="stack">
    <h3>{t("form.where")}</h3>
    {#if canPick && !pasteMode}
      <div class="row">
        <select bind:value={f.channel} aria-label={t("form.channelSelect")}>
          <option value="" disabled>{t("form.channelSelect")}</option>
          {#each list as c (c.id)}
            <option value={c.id}>{c.private ? "🔒" : "#"} {c.name}</option>
          {/each}
        </select>
        <button class="small" onclick={onreloadchannels}>{t("form.channelReload")}</button>
        <button class="small link" onclick={() => (pasteMode = true)}>{t("form.channelOther")}</button>
      </div>
      <p class="small muted"><Rich key="form.channelInvite" /></p>
    {:else}
      <input type="text" bind:value={f.channel} placeholder="https://….slack.com/archives/C0123456789" aria-label={t("form.channelPaste")} />
      <p class="small muted"><Rich key="form.channelPasteHelp" /></p>
      {#if channels?.problem === "missing_scope"}<p class="small muted"><Rich key="form.channelScope" /></p>
      {:else if channels?.problem === "error"}<p class="small muted">{t("form.channelError", { error: channels.error ?? "" })}</p>{/if}
      {#if canPick}<button class="small link" onclick={() => (pasteMode = false)}>{t("form.channelSelect")}</button>{/if}
    {/if}
    {#if problem("channel")}<p class="small bad-text">{problem("channel")}</p>{/if}
  </section>

  <!-- 2. Which messages -->
  <section class="stack">
    <h3>{t("form.what")}</h3>
    <div class="choices">
      <label class="row"><input type="radio" bind:group={f.trigger} value="keyword" /> {t("form.triggerKeyword")}</label>
      <label class="row"><input type="radio" bind:group={f.trigger} value="regex" /> {t("form.triggerRegex")}</label>
    </div>
    {#if f.trigger === "keyword"}
      <div class="narrow">
        <label for="f-kw">{t("form.keyword")}</label>
        <input id="f-kw" type="text" bind:value={f.keyword} placeholder="entry" />
      </div>
      {#if problem("keyword")}<p class="small bad-text">{problem("keyword")}</p>{/if}
      <p class="small muted"><Rich key="form.keywordExample" params={{ keyword: f.keyword || "entry" }} /></p>
    {:else}
      <label for="f-re">{t("form.regex")}</label>
      <input id="f-re" type="text" class="mono" bind:value={f.regex} placeholder={"^\\s*entry\\s+(?P<code>\\w+)"} />
      {#if problem("regex")}<p class="small bad-text">{problem("regex")}</p>{/if}
      <p class="small muted"><Rich key="form.regexHelp" /></p>
    {/if}
  </section>

  <!-- 3. What to run -->
  <section class="stack">
    <h3>{t("form.do")}</h3>
    <div class="types">
      <label class="type" class:active={f.type === "claude"}>
        <input type="radio" bind:group={f.type} value="claude" />
        <strong>{t("form.claude")}</strong>
        <span class="small muted">{t("form.claudeHelp")}</span>
      </label>
      <label class="type" class:active={f.type === "command"}>
        <input type="radio" bind:group={f.type} value="command" />
        <strong>{t("form.command")}</strong>
        <span class="small muted">{t("form.commandHelp")}</span>
      </label>
    </div>
    {#if problem("type")}<p class="small bad-text">{problem("type")}</p>{/if}

    {#if f.type === "claude"}
      <div>
        <label for="f-folder">{t("form.folder")}</label>
        <div class="row">
          <input id="f-folder" type="text" bind:value={f.folder} placeholder="~/projects/my-project" />
          <button onclick={pickFolder}>{t("form.choose")}</button>
        </div>
        {#if problem("folder")}<p class="small bad-text">{problem("folder")}</p>{/if}
      </div>
      <div>
        <label for="f-ins">{t("form.instructions")}</label>
        <textarea id="f-ins" rows="4" bind:value={f.instructions} placeholder={t("form.instructionsPlaceholder")}></textarea>
      </div>
      <div class="stack tight">
        <span class="label">{t("form.permissions")}</span>
        <p class="small muted">{t("form.permissionsHelp")}</p>
        <label class="row check"><input type="checkbox" bind:checked={f.allow_web} /> {t("form.allowWeb")}</label>
        <label class="row check"><input type="checkbox" bind:checked={f.allow_edit} /> {t("form.allowEdit")}</label>
        <label for="f-cmds">{t("form.commands")}</label>
        <textarea id="f-cmds" class="code" rows="3" bind:value={commandsText} placeholder="npm test"></textarea>
        <p class="small muted"><Rich key="form.commandsHelp" /></p>
      </div>
      <details>
        <summary class="small">{t("form.advanced")}</summary>
        <div class="stack adv">
          <div class="grid2">
            <div>
              <label for="f-model">{t("form.model")}</label>
              <select id="f-model" bind:value={f.model}>
                <option value="">{t("form.modelDefault")}</option>
                <option value="opus">Opus</option>
                <option value="sonnet">Sonnet</option>
                <option value="haiku">Haiku</option>
                {#if f.model && !["", "opus", "sonnet", "haiku"].includes(f.model)}<option value={f.model}>{f.model}</option>{/if}
              </select>
            </div>
            <div>
              <label for="f-timeout">{t("form.timeout")}</label>
              <input id="f-timeout" type="number" min="0" bind:value={f.timeout_sec} placeholder="600" />
            </div>
          </div>
          <div>
            <label for="f-prompt">{t("form.promptFile")}</label>
            <input id="f-prompt" type="text" bind:value={f.prompt_file} placeholder="prompts/slack.md" />
            {#if problem("prompt_file")}<p class="small bad-text">{problem("prompt_file")}</p>{/if}
          </div>
          <div>
            <label for="f-other">{t("form.otherTools")}</label>
            <textarea id="f-other" class="code" rows="2" bind:value={otherText} placeholder="Write(data/tmp/**)"></textarea>
          </div>
        </div>
      </details>
    {:else}
      <div>
        <label for="f-prog">{t("form.program")}</label>
        <div class="row">
          <input id="f-prog" type="text" bind:value={f.program} placeholder="~/bin/handler.sh" />
          <button onclick={pickProgram}>{t("form.choose")}</button>
        </div>
        {#if problem("program")}<p class="small bad-text">{problem("program")}</p>{/if}
      </div>
      <div>
        <label for="f-args">{t("form.args")}</label>
        <input id="f-args" type="text" bind:value={f.args} />
      </div>
      <div>
        <label for="f-folder2">{t("form.folderCommand")}</label>
        <div class="row">
          <input id="f-folder2" type="text" bind:value={f.folder} />
          <button onclick={pickFolder}>{t("form.choose")}</button>
        </div>
        {#if problem("folder")}<p class="small bad-text">{problem("folder")}</p>{/if}
      </div>
      <details>
        <summary class="small">{t("form.advanced")}</summary>
        <div class="adv narrow">
          <label for="f-timeout2">{t("form.timeout")}</label>
          <input id="f-timeout2" type="number" min="0" bind:value={f.timeout_sec} placeholder="300" />
        </div>
      </details>
    {/if}
  </section>

  <!-- 4. Name -->
  <section class="stack">
    <h3>{t("form.name")}</h3>
    <div class="narrow">
      <input type="text" bind:value={f.name} oninput={() => (nameTouched = true)} placeholder="web-app-entry" aria-label={t("form.name")} />
    </div>
    {#if problem("name")}<p class="small bad-text">{problem("name")}</p>{:else}<p class="small muted">{t("form.nameHelp")}</p>{/if}
  </section>

  {#if configProblems.length}
    <div class="error-box">
      {t("form.configProblems")}
      {#each configProblems as p}<div>• {p}</div>{/each}
    </div>
  {/if}
  {#if error}<div class="error-box">{error}</div>{/if}

  <div class="row">
    <button class="primary" disabled={saving} onclick={save}>{t("form.save")}</button>
    <button onclick={oncancel}>{t("form.cancel")}</button>
  </div>
</div>

<style>
  .editor { border-color: color-mix(in srgb, var(--accent) 45%, var(--border)); }
  section { padding-top: 12px; border-top: 1px solid var(--border); }
  h3 { font-size: 13px; }
  .narrow { max-width: 280px; }
  .choices { display: flex; flex-direction: column; gap: 4px; }
  .choices label, .check { font-weight: normal; margin: 0; gap: 6px; }
  .types { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .type {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
    cursor: pointer;
    font-weight: normal;
    margin: 0;
  }
  .type input { position: absolute; opacity: 0; pointer-events: none; }
  .type.active { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 8%, transparent); }
  .label { font-weight: 500; }
  .tight { gap: 6px; }
  .adv { margin-top: 10px; }
  .mono { font-family: ui-monospace, Menlo, monospace; }
  details summary { cursor: pointer; color: var(--muted); }
  .bad-text { color: var(--bad); }
</style>
