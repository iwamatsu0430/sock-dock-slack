<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type ChannelList, type FieldProblem, type RouteForm, type Turn } from "../lib/api";
  import { live } from "../lib/live.svelte";
  import { i18n, t } from "../lib/i18n/index.svelte";
  import Rich from "../lib/Rich.svelte";
  import { clock, elapsed, problemText, statusClass, statusLabel } from "../lib/format";

  // One rule: the form and its recent runs (docs/ia.md「ルール」).
  let { name, onclose }: { name: string; onclose: () => void } = $props();

  const blank = (): RouteForm =>
    ({
      original_name: "", name: "", channel: "", trigger: "keyword", keyword: "", regex: "", type: "claude", folder: "",
      instructions: "", allow_web: false, allow_edit: false, commands: [], other_tools: [], model: "", prompt_file: "",
      program: "", args: "", timeout_sec: 0,
    }) as RouteForm;

  let f = $state<RouteForm>(blank());
  let title = $state(t("rule.newTitle"));
  let loaded = $state(false);
  let commandsText = $state("");
  let channels = $state<ChannelList | null>(null);
  let pasteMode = $state(false);
  let resolved = $state("");
  let problems = $state<FieldProblem[]>([]);
  let error = $state("");
  let busy = $state(false);
  let turns = $state<Turn[]>([]);
  let loadedProgram = "";

  onMount(async () => {
    const [list, chs] = await Promise.all([name ? App.ListRules() : null, App.ListChannels()]);
    channels = chs;
    const row = list?.rules?.find((r) => r.form.name === name);
    if (row) {
      f = structuredClone($state.snapshot(row.form)) as RouteForm;
      if (f.trigger === "keyword") f.regex = "";
      title = row.label;
      commandsText = (f.commands ?? []).join("\n");
      loadedProgram = f.program;
    }
    const ids = (chs.channels ?? []).map((c) => c.id);
    pasteMode = ids.length === 0 || (!!f.channel && !ids.includes(f.channel));
    loaded = true;
  });

  // Recent runs reload when something changes.
  $effect(() => {
    live.changes;
    if (name) App.RuleTurns(name).then((x) => (turns = x ?? []));
  });

  // A pasted ID or link: show the channel's name when Slack tells us.
  let rtimer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => {
    const v = f.channel;
    if (!pasteMode) return;
    clearTimeout(rtimer);
    rtimer = setTimeout(async () => (resolved = v ? await App.ChannelName(v) : ""), 400);
  });

  const problem = (field: string) => problemText(problems.find((p) => p.field === field));
  const keywordShown = $derived(f.keyword.trim() || t("rule.keywordExample"));
  const defaultTimeout = $derived(f.type === "claude" ? 600 : 300);
  const howToURL = $derived(
    i18n.locale === "ja"
      ? "https://github.com/iwamatsu0430/sock-dock-slack/blob/main/README.ja.md#command"
      : "https://github.com/iwamatsu0430/sock-dock-slack#command",
  );

  async function pickFolder() {
    const p = await App.PickFolder(t("rule.folder"));
    if (p) f.folder = p;
  }
  async function pickProgram() {
    const p = await App.PickFile(t("rule.program"));
    if (p) f.program = p;
  }

  function payload(): RouteForm {
    const out = { ...$state.snapshot(f) } as RouteForm;
    out.commands = commandsText.split("\n").map((x) => x.trim()).filter(Boolean);
    out.trigger = out.regex.trim() ? "regex" : "keyword";
    if (out.type === "command" && (!out.folder || out.program !== loadedProgram)) {
      // A new program runs in its own folder (an existing cwd is kept).
      const p = out.program.trim();
      out.folder = p.includes("/") ? p.replace(/\/[^/]*$/, "") || "/" : "~";
    }
    return out;
  }

  async function save() {
    busy = true;
    error = "";
    try {
      const res = await App.SaveRoute(payload());
      problems = res.problems ?? [];
      if (res.ok) onclose();
      else if (res.config_problems?.length) error = res.config_problems.join("\n");
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function remove() {
    if (!confirm(t("rule.deleteConfirm"))) return;
    const res = await App.DeleteRoute(f.original_name);
    if (res.ok) onclose();
    else error = (res.config_problems ?? []).join("\n");
  }

  async function stop(id: number) {
    try {
      await App.StopTurn(id);
    } catch (e) {
      error = errorText(e);
    }
  }
</script>

<div class="back-row"><button class="back" onclick={onclose}>{t("rule.back")}</button></div>
<h1 class="title">{title}</h1>

{#if loaded}
  <div class="sec first">
    <h2>{t("rule.channel")}</h2>
    <div class="ctl">
      {#if !pasteMode}
        <select bind:value={f.channel}>
          <option value="" disabled>{t("rule.channelPick")}</option>
          {#each channels?.channels ?? [] as c (c.id)}<option value={c.id}>{c.private ? "🔒" : "#"}{c.name}</option>{/each}
        </select>
        <p class="small muted">{t("rule.channelListHelp")}</p>
      {:else}
        <input type="text" bind:value={f.channel} placeholder="C0123456789" class:invalid={problem("channel")} />
        {#if resolved}<p class="small muted">{resolved}</p>{/if}
        {#if channels?.problem === "missing_scope"}<p class="small muted"><Rich key="rule.channelNoScope" /></p>
        {:else if channels?.problem === "error"}<p class="small muted">{t("rule.channelError", { error: channels.error ?? "" })}</p>
        {:else if !channels?.channels?.length}<p class="small muted">{t("rule.channelPaste")}</p>{/if}
      {/if}
      {#if problem("channel")}<p class="small bad-t">{problem("channel")}</p>{/if}
      <p class="small">{t("rule.channelMembers")}</p>
    </div>
  </div>

  <div class="sec">
    <h2>{t("rule.keyword")}</h2>
    <div class="ctl">
      <input type="text" class="kw" bind:value={f.keyword} class:invalid={problem("keyword")} disabled={!!f.regex.trim()} />
      {#if problem("keyword")}<p class="small bad-t">{problem("keyword")}</p>
      {:else if f.regex.trim()}<p class="small muted">{t("rule.regexInUse")}</p>
      {:else}<p class="small muted">{t("rule.keywordHelp", { keyword: keywordShown })}</p>{/if}
    </div>
  </div>

  <div class="sec">
    <h2>{t("rule.run")}</h2>
    <div class="ctl">
      <div class="seg">
        <button class:on={f.type === "claude"} onclick={() => (f.type = "claude")}>{t("rule.kindClaude")}</button>
        <button class:on={f.type === "command"} onclick={() => (f.type = "command")}>{t("rule.kindCommand")}</button>
      </div>
      {#if problem("type")}<p class="small bad-t">{problem("type")}</p>{/if}
      {#if f.type === "claude"}
        <div class="stack body">
          <div><label class="f small" for="r-agent">{t("rule.agent")}</label>
            <select id="r-agent" class="narrow"><option>{t("agent.claude")}</option></select></div>
          <div><label class="f small" for="r-folder">{t("rule.folder")}</label>
            <div class="row"><input id="r-folder" type="text" bind:value={f.folder} class:invalid={problem("folder")} /><button onclick={pickFolder}>{t("rule.choose")}</button></div>
            {#if problem("folder")}<p class="small bad-t">{problem("folder")}</p>{/if}</div>
          <div><label class="f small" for="r-inst">{t("rule.instructions")}</label>
            <textarea id="r-inst" rows="2" bind:value={f.instructions}></textarea></div>
          <div><span class="f small label">{t("rule.permissions")}</span>
            <label class="check"><input type="checkbox" bind:checked={f.allow_web} /> {t("rule.allowWeb")}</label>
            <label class="check"><input type="checkbox" bind:checked={f.allow_edit} /> {t("rule.allowEdit")}</label>
            <label class="f small sub" for="r-cmds">{t("rule.commands")}</label>
            <textarea id="r-cmds" rows="2" bind:value={commandsText} placeholder={t("rule.commandsPlaceholder")}></textarea>
            <p class="small muted note">{t("rule.permissionsNote")}</p></div>
        </div>
      {:else}
        <div class="stack body">
          <div><label class="f small" for="r-prog">{t("rule.program")}</label>
            <div class="row"><input id="r-prog" type="text" bind:value={f.program} placeholder="~/bin/handler.sh" class:invalid={problem("program")} /><button onclick={pickProgram}>{t("rule.choose")}</button></div>
            {#if problem("program")}<p class="small bad-t">{problem("program")}</p>{/if}</div>
          <div><label class="f small" for="r-args">{t("rule.args")}</label><input id="r-args" type="text" bind:value={f.args} /></div>
          <p class="small muted">{t("rule.programNote")}<button class="link small" onclick={() => App.OpenURL(howToURL)}>{t("rule.howTo")}</button></p>
        </div>
      {/if}
    </div>
  </div>

  <details class="more" open={!!f.regex || !!f.model || !!f.timeout_sec}>
    <summary>{t("rule.options")}</summary>
    {#if f.type === "claude"}
      <div class="sec first"><h2>{t("rule.model")}</h2><div class="ctl">
        <select class="narrow" bind:value={f.model}>
          <option value="">{t("rule.modelDefault")}</option>
          <option value="opus">opus</option><option value="sonnet">sonnet</option><option value="haiku">haiku</option>
          {#if f.model && !["opus", "sonnet", "haiku"].includes(f.model)}<option value={f.model}>{f.model}</option>{/if}
        </select></div></div>
    {/if}
    <div class="sec" class:first={f.type !== "claude"}><h2>{t("rule.timeout")}</h2><div class="ctl">
      <div class="row"><input type="number" min="0" class="num" bind:value={f.timeout_sec} placeholder={String(defaultTimeout)} /><span class="muted">{t("rule.seconds")}</span></div></div></div>
    <div class="sec"><h2>{t("rule.regex")}</h2><div class="ctl">
      <input type="text" class="mono" bind:value={f.regex} placeholder={t("rule.regexPlaceholder")} class:invalid={problem("regex")} />
      {#if problem("regex")}<p class="small bad-t">{problem("regex")}</p>{/if}</div></div>
  </details>

  {#if error}<p class="small bad-t">{error}</p>{/if}
  <div class="formfoot">
    {#if f.original_name}<button class="danger" onclick={remove}>{t("rule.delete")}</button>{/if}
    <span class="spacer"></span>
    <button onclick={onclose}>{t("rule.cancel")}</button>
    <button class="primary" disabled={busy} onclick={save}>{t("rule.save")}</button>
  </div>

  {#if name && turns.length}
    <div class="runs">
      <h2>{t("runs.title")}</h2>
      <div class="card">
        <table>
          <thead><tr><th>{t("runs.time")}</th><th>{t("runs.result")}</th><th></th></tr></thead>
          <tbody>
            {#each turns as r (r.id)}
              <tr>
                <td class="nowrap">{clock(r.started_at, live.now)}</td>
                <td>
                  {#if r.status === "running"}{t("runs.running", { elapsed: elapsed(r.started_at, live.now) })}
                  {:else}<span class={statusClass(r.status)}>{statusLabel(r.status)}</span>
                    {#if r.status === "timeout"}<div class="muted">{t("runs.timeoutReason", { sec: f.timeout_sec || defaultTimeout })}</div>
                    {:else if r.status === "error" && r.error}<div class="muted reason">{r.error}</div>{/if}
                  {/if}
                </td>
                <td class="nowrap actions">
                  {#if r.status === "running"}<button class="small" onclick={() => stop(r.id)}>{t("runs.stop")}</button>{/if}
                  {#if r.permalink}<button class="link small" onclick={() => App.OpenURL(r.permalink!)}>{t("runs.thread")}</button>{/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
{/if}

<style>
  .back-row { min-height: 28px; }
  .back { border: none; background: none; padding: 0; color: var(--accent); }
  .back:hover { background: none; text-decoration: underline; }
  .title { margin-bottom: 6px; }
  .sec { padding: 14px 0; border-top: 1px solid var(--border); display: grid; grid-template-columns: 110px 1fr; gap: 6px 16px; }
  .sec.first { border-top: none; }
  .sec > h2 { padding-top: 5px; }
  .ctl { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .kw, .narrow { max-width: 200px; }
  .num { max-width: 90px; }
  .mono { font-family: ui-monospace, Menlo, monospace; }
  .seg { display: inline-flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; width: fit-content; }
  .seg button { border: none; border-radius: 0; padding: 4px 14px; }
  .seg button + button { border-left: 1px solid var(--border); }
  .seg button.on, .seg button.on:hover { background: var(--accent); color: var(--accent-text); }
  .body { gap: 10px; margin-top: 4px; }
  .label { display: block; font-weight: 500; margin-bottom: 4px; }
  .sub { margin-top: 8px; font-weight: normal; }
  .note { margin-top: 4px; }
  details.more summary { cursor: pointer; color: var(--muted); font-size: 12px; padding: 10px 0; }
  .formfoot { display: flex; gap: 8px; padding: 14px 0; border-top: 1px solid var(--border); }
  .runs { margin-top: 6px; }
  .runs h2 { margin-bottom: 8px; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th { text-align: left; font-weight: 500; color: var(--muted); padding: 6px 12px; border-bottom: 1px solid var(--border); white-space: nowrap; }
  td { padding: 7px 12px; border-bottom: 1px solid var(--border); vertical-align: top; }
  tr:last-child td { border-bottom: none; }
  .nowrap { white-space: nowrap; }
  .actions { text-align: right; }
  .reason { white-space: pre-wrap; word-break: break-word; max-height: 4.5em; overflow: hidden; }
</style>
