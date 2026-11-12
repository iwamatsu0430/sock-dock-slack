import { Events } from "@wailsio/runtime";
import { App, SockdockActivity, SockdockSnapshot, type Snapshot, type Turn } from "./api";
import { t } from "./i18n/index.svelte";

/** Live gateway state shared by all components. */
export const live = $state({
  snap: null as Snapshot | null,
  turns: [] as Turn[],
  now: Date.now(),
});

let started = false;

async function loadTurns() {
  if (!live.snap?.running) return;
  try {
    live.turns = (await App.Turns(30)) ?? [];
  } catch {
    /* gateway went away; the next snapshot says so */
  }
}

/** Starts listening to gateway events (idempotent). */
export function startLive() {
  if (started) return;
  started = true;
  App.GetSnapshot().then((s) => {
    live.snap = s;
    loadTurns();
  });
  Events.On(SockdockSnapshot, (ev) => {
    const wasRunning = live.snap?.running;
    live.snap = ev.data as Snapshot;
    if (!wasRunning && live.snap?.running) loadTurns();
  });
  Events.On(SockdockActivity, (ev) => {
    const t = (ev.data as { type?: string })?.type;
    if (t === "turn_started" || t === "turn_finished") loadTurns();
  });
  setInterval(() => (live.now = Date.now()), 1000);
}

/** Connection summary used by headers and the tray. */
export function connection(snap: Snapshot | null): { level: "ok" | "warn" | "bad" | "idle"; text: string } {
  if (!snap) return { level: "idle", text: t("conn.checking") };
  if (!snap.running) return { level: "bad", text: t("conn.notRunning") };
  const s = snap.status!;
  if (!s.connected) return { level: "warn", text: s.last_error ? t("conn.notConnected", { error: s.last_error }) : t("conn.connecting") };
  if ((s.running ?? []).length > 0) return { level: "ok", text: t("conn.connectedRunning", { n: s.running!.length }) };
  return { level: "ok", text: t("conn.connected") };
}

/** Describes where the gateway runs, or why it does not. */
export function gatewayText(g: { state: string; error?: string; missing?: string } | undefined): string {
  switch (g?.state) {
    case "running":
      return t("gw.running");
    case "external":
      return t("gw.external");
    case "setup":
      return g.missing === "config" ? t("gw.setupConfig") : t("gw.setupTokens");
    case "stopped":
      return t("gw.stopped");
    case "error":
      return t("gw.error", { error: g.error ?? "" });
    default:
      return t("conn.checking");
  }
}
