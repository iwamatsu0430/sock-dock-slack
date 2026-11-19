<script lang="ts">
  import { onMount } from "svelte";
  import { App, errorText, type ChannelList, type FieldProblem, type Member, type RouteForm, type Turn } from "../lib/api";
  import { hasScope, live, reasonCodexSignedOut, reasonSignedOut, signInAgent } from "../lib/live.svelte";
  import { i18n, t } from "../lib/i18n/index.svelte";
  import type { MessageKey } from "../lib/i18n/en";
  import Rich from "../lib/Rich.svelte";
  import { clock, elapsed, problemText, statusClass, statusLabel } from "../lib/format";

  // One rule: the form and its recent runs.
  let { name, onclose }: { name: string; onclose: () => void } = $props();

  const blank = (): RouteForm =>
    ({
      original_name: "", name: "", channel: "", trigger: "starts_with", keyword: "", reaction_removed: false, mention_only: false, restricted: false, allowed_users: [],
      type: "claude", folder: "",
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
  // 実行できる人: the chosen people, in order.
  let people = $state<Member[]>([]);
  let personQuery = $state("");
  let suggestions = $state<Member[]>([]);
  // Why names cannot be listed ("missing_scope", "error", "no_token"); then
  // the field takes member IDs instead.
  let memberProblem = $state("");
  let memberError = $state("");
  let membersChecked = false;
  let loadedProgram = "";
  // The saved condition, for the "no runs yet" hint (not the one being edited).
  let saved = $state({ trigger: "", keyword: "", channel: "", mention: false });
  // The owner ("本人"): a new DM rule starts with them chosen.
  let owner = $state<Member | null>(null);

  onMount(async () => {
    const [list, chs, own] = await Promise.all([name ? App.ListRules() : null, App.ListChannels(), App.GetOwner().catch(() => null)]);
    channels = chs;
    owner = own?.owner ?? null;
    const row = list?.rules?.find((r) => r.form.name === name);
    if (row) {
      f = structuredClone($state.snapshot(row.form)) as RouteForm;
      title = row.label;
      commandsText = (f.commands ?? []).join("\n");
      people = (row.users ?? []).map((u) => ({ ...u }));
      loadedProgram = f.program;
      saved = { trigger: f.trigger, keyword: f.keyword, channel: f.channel, mention: !!f.mention_only };
    }
    choosePasteMode();
    loaded = true;
  });

  // Paste an ID or link when the list is empty or the rule's channel is not in it.
  function choosePasteMode() {
    const ids = (channels?.channels ?? []).map((c) => c.id);
    pasteMode = f.channel !== "dm" && (ids.length === 0 || (!!f.channel && !ids.includes(f.channel)));
  }

  // DMs to the Slack app: a new DM rule starts
  // with the owner chosen, or with nobody chosen when there is no owner.
  const isDM = $derived(f.channel === "dm");
  function channelChanged() {
    if (f.channel === "dm" && !name && !f.restricted) {
      f.restricted = true;
      people = owner ? [owner] : [];
    }
  }


  // After adding the Slack app to a channel, show the list without reopening.
  let reloading = $state(false);
  async function reloadChannels() {
    reloading = true;
    try {
      channels = await App.ListChannels();
      choosePasteMode();
    } finally {
      reloading = false;
    }
  }

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

  // Ask once whether names can be listed, when the field first needs them.
  $effect(() => {
    if (!f.restricted || membersChecked) return;
    membersChecked = true;
    App.ListMembers("").then((r) => {
      memberProblem = r.problem ?? "";
      memberError = r.error ?? "";
    });
  });
  let ptimer: ReturnType<typeof setTimeout> | undefined;
  function searchPeople() {
    clearTimeout(ptimer);
    const q = personQuery.trim();
    if (!q || memberProblem) {
      suggestions = [];
      return;
    }
    ptimer = setTimeout(async () => {
      const r = await App.ListMembers(q);
      if (r.problem) {
        memberProblem = r.problem;
        memberError = r.error ?? "";
      }
      const chosen = new Set(people.map((p) => p.id));
      suggestions = (r.members ?? []).filter((m) => !chosen.has(m.id));
    }, 200);
  }
  function addPerson(m: Member) {
    if (!people.some((p) => p.id === m.id)) people = [...people, m];
    personQuery = "";
    suggestions = [];
  }
  function personKey(e: KeyboardEvent) {
    const v = personQuery.trim();
    if (e.key === "Enter") {
      e.preventDefault();
      if (suggestions.length) addPerson(suggestions[0]);
      else if (/^[UW][A-Z0-9]{2,}$/i.test(v)) addPerson({ id: v.toUpperCase(), name: "" } as Member);
    } else if (e.key === "Escape") {
      suggestions = [];
    } else if (e.key === "Backspace" && !v && people.length) {
      people = people.slice(0, -1);
    }
  }
  // Claude Code or Codex. Switching agents keeps the
  // web and edit choices; Claude Code's commands stay in commandsText.
  const isAgent = $derived(f.type === "claude" || f.type === "codex");
  let lastAgent = $state("claude");
  $effect(() => {
    if (f.type === "claude" || f.type === "codex") lastAgent = f.type;
  });
  const agentSignedOut = $derived(
    (live.snap?.status?.signed_in?.[f.type] ?? (f.type === "claude" ? live.snap?.status?.claude_signed_in : undefined)) === false,
  );
  // Everyone in the channel can make the agent run commands or edit files.
  // Codex always runs commands (in its sandbox).
  const openToAll = $derived(
    f.restricted || !isAgent
      ? ""
      : f.type === "codex" || commandsText.trim()
        ? "commands"
        : f.allow_edit
          ? "edit"
          : "",
  );
  const problem = (field: string) => problemText(problems.find((p) => p.field === field));
  // SockDock knows which channels' posts reached it since it connected; when
  // this rule's channel has none, the problem is delivery, not the condition.
  // A reaction rule needs the channel's reactions, not its posts.
  const notHeard = $derived(
    !!live.snap?.status?.connected && !!saved.channel &&
      !((saved.trigger === "reaction" ? live.snap?.status?.heard_reactions : live.snap?.status?.heard) ?? []).includes(saved.channel),
  );
  const triggers = ["starts_with", "contains", "match", "all_posts", "reaction"];
  const isReaction = $derived(f.trigger === "reaction");
  const reactionHelp = $derived(
    t(`rule.reaction${f.reaction_removed ? "Removed" : ""}Help${isDM ? "DM" : ""}` as MessageKey) +
      (f.reaction_removed ? (i18n.locale === "ja" ? "" : " ") + t(isAgent ? "rule.reactionToldAgent" : "rule.reactionToldProgram") : ""),
  );
  const channelShown = $derived(
    (() => {
      if (saved.channel === "dm") return t("rule.channelDM");
      const c = channels?.channels?.find((x) => x.id === saved.channel);
      return c ? (c.private ? "🔒" : "#") + c.name : resolved || saved.channel;
    })(),
  );
  const defaultTimeout = $derived(isAgent ? 600 : 300);
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
    out.allowed_users = people.map((p) => p.id);
    if (out.channel === "dm" || out.trigger === "reaction") out.mention_only = false;
    if (out.trigger !== "reaction") out.reaction_removed = false;
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
        <select bind:value={f.channel} onchange={channelChanged}>
          <option value="" disabled>{t("rule.channelPick")}</option>
          <option value="dm">{t("rule.channelDM")}</option>
          <option disabled>──────────</option>
          {#each channels?.channels ?? [] as c (c.id)}<option value={c.id}>{c.private ? "🔒" : "#"}{c.name}</option>{/each}
        </select>
        {#if !isDM}<p class="small muted">{t("rule.channelListHelp")}</p>
        {:else if !hasScope(live.snap, "im:history")}
          <p class="small bad-t"><Rich key="rule.dmNoScope" /></p>
        {/if}
      {:else}
        <!-- DMs do not need the channel list, so they can be chosen here too. -->
        <select class="narrow" value={isDM ? "dm" : "other"}
          onchange={(e) => { if ((e.currentTarget as HTMLSelectElement).value === "dm") { f.channel = "dm"; channelChanged(); } else f.channel = ""; }}>
          <option value="dm">{t("rule.channelDM")}</option>
          <option value="other">{t("rule.channelOther")}</option>
        </select>
      {/if}
      {#if pasteMode && isDM && !hasScope(live.snap, "im:history")}
        <p class="small bad-t"><Rich key="rule.dmNoScope" /></p>
      {:else if pasteMode && !isDM}
        <input type="text" bind:value={f.channel} placeholder="C0123456789" class:invalid={problem("channel")} />
        {#if resolved}<p class="small muted">{resolved}</p>{/if}
        {#if channels?.problem === "missing_scope"}<p class="small muted"><Rich key="rule.channelNoScope" /></p>
        {:else if channels?.problem === "error"}<p class="small muted">{t("rule.channelError", { error: channels.error ?? "" })}</p>
        {:else if !channels?.channels?.length}<p class="small muted"><Rich key="rule.channelPaste" />
          <button class="link small" disabled={reloading} onclick={reloadChannels}>{t("rule.reloadChannels")}</button></p>{/if}
      {/if}
      {#if problem("channel")}<p class="small bad-t">{problem("channel")}</p>{/if}
    </div>
  </div>
  <div class="sec">
    <h2>{t("rule.who")}</h2>
    <div class="ctl">
      <div class="seg">
        <button class:on={!f.restricted} onclick={() => (f.restricted = false)}>{t("rule.whoAll")}</button>
        <button class:on={f.restricted} onclick={() => (f.restricted = true)}>{t("rule.whoPicked")}</button>
      </div>
      {#if !f.restricted}
        {#if openToAll === "commands"}<p class="small bad-t">{isDM ? t("rule.openCommandsDM") : t("rule.openCommands")}</p>
        {:else if openToAll === "edit"}<p class="small bad-t">{isDM ? t("rule.openEditDM") : t("rule.openEdit")}</p>
        {:else}<p class="small">{isDM ? t("rule.workspaceMembers") : t("rule.channelMembers")}</p>{/if}
      {:else}
        <div class="chips" class:invalid={problem("allowed_users")}>
          {#each people as p (p.id)}
            <span class="chip" class:id={!p.name}>{p.name || p.id}<button title={t("rule.removePerson")} aria-label={t("rule.removePerson")}
              onclick={() => (people = people.filter((x) => x.id !== p.id))}>×</button></span>
          {/each}
          <input type="text" bind:value={personQuery} oninput={searchPeople} onkeydown={personKey}
            onblur={() => setTimeout(() => (suggestions = []), 150)} autocomplete="off"
            placeholder={memberProblem ? t("rule.memberId") : t("rule.addPerson")} />
          {#if suggestions.length}
            <div class="sugg">
              {#each suggestions as m (m.id)}
                <button class="opt" onmousedown={(e) => { e.preventDefault(); addPerson(m); }}>{m.name}{#if m.real_name} <span class="muted small">{m.real_name}</span>{/if}</button>
              {/each}
            </div>
          {/if}
        </div>
        {#if problem("allowed_users")}<p class="small bad-t">{problem("allowed_users")}</p>{/if}
        {#if memberProblem === "missing_scope"}<p class="small muted"><Rich key="rule.usersNoScope" /></p>
        {:else if memberProblem === "error"}<p class="small muted">{t("rule.usersError", { error: memberError })}</p>{/if}
      {/if}
    </div>
  </div>

  <div class="sec">
    <h2>{t("rule.trigger")}</h2>
    <div class="ctl">
      <div class="row">
        <select class="trigger" bind:value={f.trigger}>
          {#each triggers as k (k)}<option value={k}>{t(`rule.trigger.${k}` as MessageKey)}</option>{/each}
        </select>
        {#if f.trigger !== "all_posts"}
          <input type="text" class="kw" class:mono={f.trigger === "match"} bind:value={f.keyword} class:invalid={problem("keyword")}
            placeholder={f.trigger === "match" ? t("rule.regexPlaceholder") : isReaction ? ":eyes:" : t("rule.keywordPlaceholder")} />
        {/if}
      </div>
      {#if isReaction}<label class="check"><input type="checkbox" bind:checked={f.reaction_removed} /> {t("rule.reactionRemoved")}</label>
      {:else if !isDM}<label class="check"><input type="checkbox" bind:checked={f.mention_only} /> {t("rule.mentionOnly")}</label>{/if}
      {#if f.trigger !== "all_posts" && problem("keyword")}<p class="small bad-t">{problem("keyword")}</p>{/if}
      {#if f.trigger === "all_posts"}<p class="small muted">{isDM ? t("rule.allPostsDMHelp") : f.mention_only ? t("rule.allPostsMentionHelp") : t("rule.allPostsHelp")}</p>
      {:else if isReaction && !hasScope(live.snap, "reactions:read")}<p class="small bad-t"><Rich key="rule.reactionNoScope" /></p>
      {:else if isReaction}<p class="small muted">{reactionHelp}</p>{/if}
    </div>
  </div>

  <div class="sec">
    <h2>{t("rule.run")}</h2>
    <div class="ctl">
      <div class="seg">
        <button class:on={isAgent} onclick={() => (f.type = isAgent ? f.type : lastAgent)}>{t("rule.kindClaude")}</button>
        <button class:on={f.type === "command"} onclick={() => (f.type = "command")}>{t("rule.kindCommand")}</button>
      </div>
      {#if problem("type")}<p class="small bad-t">{problem("type")}</p>{/if}
      {#if isAgent}
        <div class="stack body">
          <div><label class="f small" for="r-agent">{t("rule.agent")}</label>
            <select id="r-agent" class="narrow" bind:value={f.type} onchange={() => (f.model = "")}>
              <option value="claude">{t("agent.claude")}</option>
              <option value="codex">{t("agent.codex")}</option>
            </select>
            {#if agentSignedOut}
              <p class="small bad-t row signin">{t(f.type === "codex" ? "banner.codex" : "banner.claude")}<button class="small" onclick={() => signInAgent(f.type).catch((e) => (error = errorText(e)))}>{t("claude.signIn")}</button></p>
            {/if}</div>
          <div><label class="f small" for="r-folder">{t("rule.folder")}</label>
            <div class="row"><input id="r-folder" type="text" bind:value={f.folder} class:invalid={problem("folder")} /><button onclick={pickFolder}>{t("rule.choose")}</button></div>
            {#if problem("folder")}<p class="small bad-t">{problem("folder")}</p>{/if}</div>
          <div><label class="f small" for="r-inst">{t("rule.instructions")}</label>
            <textarea id="r-inst" rows="2" bind:value={f.instructions}></textarea></div>
          <div><span class="f small label">{t("rule.permissions")}</span>
            <label class="check"><input type="checkbox" bind:checked={f.allow_web} /> {t("rule.allowWeb")}</label>
            <label class="check"><input type="checkbox" bind:checked={f.allow_edit} /> {t("rule.allowEdit")}</label>
            {#if f.type === "codex"}
              <p class="small muted note">{t("rule.codexCommands")}</p>
            {:else}
              <label class="f small sub" for="r-cmds">{t("rule.commands")}</label>
              <textarea id="r-cmds" rows="2" bind:value={commandsText} placeholder={t("rule.commandsPlaceholder")}></textarea>
              <p class="small muted note">{t("rule.permissionsNote")}</p>
            {/if}</div>
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

  <details class="more" open={!!f.model || !!f.timeout_sec}>
    <summary>{t("rule.options")}</summary>
    {#if f.type === "codex"}
      <div class="sec first"><h2>{t("rule.model")}</h2><div class="ctl">
        <input type="text" class="narrow" bind:value={f.model} placeholder={t("rule.modelDefault")} /></div></div>
    {:else if f.type === "claude"}
      <div class="sec first"><h2>{t("rule.model")}</h2><div class="ctl">
        <select class="narrow" bind:value={f.model}>
          <option value="">{t("rule.modelDefault")}</option>
          <option value="opus">opus</option><option value="sonnet">sonnet</option><option value="haiku">haiku</option>
          {#if f.model && !["opus", "sonnet", "haiku"].includes(f.model)}<option value={f.model}>{f.model}</option>{/if}
        </select></div></div>
    {/if}
    <div class="sec" class:first={!isAgent}><h2>{t("rule.timeout")}</h2><div class="ctl">
      <div class="row"><input type="number" min="0" class="num" bind:value={f.timeout_sec} placeholder={String(defaultTimeout)} /><span class="muted">{t("rule.seconds")}</span></div></div></div>
  </details>

  {#if error}<p class="small bad-t">{error}</p>{/if}
  <div class="formfoot">
    {#if f.original_name}<button class="danger" onclick={remove}>{t("rule.delete")}</button>{/if}
    <span class="spacer"></span>
    <button onclick={onclose}>{t("rule.cancel")}</button>
    <button class="primary" disabled={busy} onclick={save}>{t("rule.save")}</button>
  </div>

  {#if name && saved.trigger && !turns.length}
    <div class="runs">
      <h2>{t("runs.title")}</h2>
      {#if saved.trigger === "reaction"}
        <p class="small muted">{t(saved.channel === "dm" ? "runs.emptyReactionDM" : "runs.emptyReaction", { channel: channelShown, keyword: saved.keyword })}</p>
      {:else}
        <p class="small muted">{t(`${saved.channel === "dm" ? "runs.emptyDM" : saved.mention ? "runs.emptyMention" : "runs.empty"}.${saved.trigger}` as MessageKey, { channel: channelShown, keyword: saved.keyword })}</p>
      {/if}
      {#if notHeard}
        <p class="small bad-t heard">{t(`runs.notHeard${saved.trigger === "reaction" ? "Reaction" : ""}${saved.channel === "dm" ? "DM" : ""}` as MessageKey)}</p>
        <details class="hint small">
          <summary>{t("runs.notHeardHelp")}</summary>
          <ul>
            {#if saved.trigger === "reaction"}
              {#if saved.channel !== "dm"}<li>{t("runs.checkInvite")}</li>{/if}
              <li>{t("runs.checkSameApp")}</li>
              <li><Rich key="runs.checkEventsReaction" /></li>
            {:else if saved.channel === "dm"}
              <li>{t("runs.checkMessagesTab")}</li>
              <li>{t("runs.checkSameApp")}</li>
              <li><Rich key="runs.checkEventsDM" /></li>
            {:else}
              <li>{t("runs.checkInvite")}</li>
              <li>{t("runs.checkSameApp")}</li>
              <li><Rich key="runs.checkEvents" /></li>
            {/if}
          </ul>
        </details>
      {/if}
    </div>
  {:else if name}
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
                    {:else if r.status === "error" && r.error === reasonSignedOut}<div class="muted">{t("runs.signedOut")}</div>
                    {:else if r.status === "error" && r.error === reasonCodexSignedOut}<div class="muted">{t("runs.codexSignedOut")}</div>
                    {:else if r.status === "error" && r.error}<div class="muted reason">{r.error}</div>{/if}
                  {/if}
                  {#if r.file_problem === "missing_scope"}<div class="muted"><Rich key="runs.filesNoScope" /></div>{/if}
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
  .narrow { max-width: 200px; }
  .trigger { width: auto; flex: none; }
  .kw { flex: 1; min-width: 0; max-width: 240px; }
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
  .heard { margin-top: 6px; }
  details.hint { margin-top: 4px; }
  details.hint summary { cursor: pointer; color: var(--muted); }
  details.hint ul { margin: 6px 0 0; padding-left: 18px; display: flex; flex-direction: column; gap: 4px; }
  .signin { margin-top: 6px; gap: 8px; }
  .chips { position: relative; display: flex; flex-wrap: wrap; gap: 6px; align-items: center; border: 1px solid var(--border);
    border-radius: 6px; padding: 4px 6px; background: var(--panel); }
  .chips.invalid { border-color: var(--bad); }
  .chip { display: inline-flex; align-items: center; background: var(--panel-2); border: 1px solid var(--border); border-radius: 4px; padding: 0 0 0 8px; font-size: 12px; }
  .chip.id { font-family: ui-monospace, Menlo, monospace; }
  .chip button { border: none; background: none; padding: 0 6px; color: var(--muted); font-size: 12px; }
  .chips input { border: none; flex: 1; min-width: 140px; padding: 2px; background: transparent; box-shadow: none; }
  .chips input:focus { outline: none; }
  .sugg { position: absolute; left: -1px; right: -1px; top: calc(100% + 4px); z-index: 5; background: var(--panel); border: 1px solid var(--border);
    border-radius: 6px; box-shadow: 0 8px 24px rgba(0, 0, 0, .15); padding: 4px; display: flex; flex-direction: column; }
  .sugg .opt { border: none; background: none; text-align: left; padding: 4px 8px; border-radius: 4px; }
  .sugg .opt:hover { background: var(--accent); color: var(--accent-text); }
  .sugg .opt:hover .muted { color: inherit; }
  .reason { white-space: pre-wrap; word-break: break-word; max-height: 4.5em; overflow: hidden; }
</style>
