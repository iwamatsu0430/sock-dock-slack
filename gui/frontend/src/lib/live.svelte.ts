import { Events } from "@wailsio/runtime";
import { App, SockdockActivity, SockdockSnapshot, type Snapshot } from "./api";

/** Live gateway state shared by all components. */
export const live = $state({
  snap: null as Snapshot | null,
  /** Bumps when runs, rules or their state change, so views reload. */
  changes: 0,
  now: Date.now(),
});

let started = false;

/** Starts listening to gateway events (idempotent). */
export function startLive() {
  if (started) return;
  started = true;
  App.GetSnapshot().then((s) => (live.snap = s));
  Events.On(SockdockSnapshot, (ev) => {
    const before = live.snap;
    live.snap = ev.data as Snapshot;
    if (before?.running !== live.snap?.running || before?.gateway?.state !== live.snap?.gateway?.state) live.changes++;
  });
  Events.On(SockdockActivity, (ev) => {
    const t = (ev.data as { type?: string })?.type;
    if (t === "turn_started" || t === "turn_finished" || t === "config_reloaded" || t === "route_paused") live.changes++;
    if (t === "config_reloaded") checkClaude();
  });
  setInterval(() => (live.now = Date.now()), 1000);
  // Claude Code's sign-in is checked when the window comes back (docs/ia.md §11-21).
  checkClaude();
  window.addEventListener("focus", checkClaude);
}

// After 「ターミナルでサインイン」, checks skip the gateway's recent answer until
// Claude Code is signed in.
let signingIn = false;

function checkClaude() {
  if (live.snap?.status?.claude_signed_in === true) signingIn = false;
  App.CheckClaude(signingIn).catch(() => {});
}

/** Opens Terminal to sign in to the Claude Code that rules run. */
export async function signInClaude() {
  signingIn = true;
  await App.SignInClaude();
}

/** True when Claude Code is signed out and a rule that is on runs it. */
export function claudeSignedOut(snap: Snapshot | null): boolean {
  const st = snap?.status;
  if (!snap?.running || st?.claude_signed_in !== false) return false;
  return (st.routes ?? []).some((r) => r.type === "claude" && !r.paused);
}

/** The run error SockDock records when Claude Code is not signed in (handler.ReasonSignedOut). */
export const reasonSignedOut = "Claude Code is not signed in";

/** True until both tokens are set (the window shows setup). */
export function needsSetup(snap: Snapshot | null): boolean {
  return snap?.gateway?.state === "setup" && snap.gateway.missing === "tokens";
}

const authError = /invalid_auth|not_authed|token_revoked|token_expired|account_inactive/;

/**
 * Which "can't connect" banner to show, if any: "bot" or "app" when a token
 * stopped working, "retry" when Slack is unreachable for a while.
 */
export function banner(snap: Snapshot | null, now: number): "bot" | "app" | "retry" | null {
  if (!snap || needsSetup(snap)) return null;
  const g = snap.gateway;
  if (g?.state === "error") {
    const err = g.error ?? "";
    if (authError.test(err)) return err.includes("auth.test") ? "bot" : "app";
    return "retry";
  }
  if (snap.running && snap.status && !snap.status.connected) {
    const since = Date.parse(snap.status.last_disconnect_at ?? snap.status.started_at);
    if (now - since < 15000) return null;
    return authError.test(snap.status.last_error ?? "") ? "app" : "retry";
  }
  return null;
}
