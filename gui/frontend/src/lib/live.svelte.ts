import { Events } from "@wailsio/runtime";
import { App, SockdockActivity, SockdockSnapshot, type Snapshot, type Turn } from "./api";

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
  if (!snap) return { level: "idle", text: "Checking…" };
  if (!snap.running) return { level: "bad", text: "Gateway not running" };
  const s = snap.status!;
  if (!s.connected) return { level: "warn", text: s.last_error ? `Not connected: ${s.last_error}` : "Connecting to Slack…" };
  if ((s.running ?? []).length > 0) return { level: "ok", text: `Connected · ${s.running!.length} running` };
  return { level: "ok", text: "Connected" };
}
