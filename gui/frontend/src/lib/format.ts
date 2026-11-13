import { i18n, t } from "./i18n/index.svelte";
import type { MessageKey } from "./i18n/en";

export function ago(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return "";
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (s < 60) return t("time.secondsAgo", { n: s });
  if (s < 3600) return t("time.minutesAgo", { n: Math.floor(s / 60) });
  if (s < 86400) return t("time.hoursAgo", { n: Math.floor(s / 3600) });
  return t("time.daysAgo", { n: Math.floor(s / 86400) });
}

export function elapsed(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return "";
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  return s < 60 ? t("time.seconds", { s }) : t("time.minutesSeconds", { m: Math.floor(s / 60), s: s % 60 });
}

/** "14:02", "Yesterday 18:20" or "Nov 3 18:20". */
export function clock(iso: string, now = Date.now()): string {
  const d = new Date(iso);
  const hm = d.toLocaleTimeString(i18n.locale, { hour: "2-digit", minute: "2-digit" });
  const today = new Date(now);
  if (d.toDateString() === today.toDateString()) return hm;
  const y = new Date(now - 86400000);
  if (d.toDateString() === y.toDateString()) return t("time.yesterday", { time: hm });
  return d.toLocaleString(i18n.locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

/** CSS class for a run status. */
export function statusClass(status: string): string {
  if (status === "ok") return "ok-t";
  if (status === "error" || status === "timeout") return "bad-t";
  return "";
}

/** Localized label for a run status. */
export function statusLabel(status: string): string {
  const key = `status.${status}` as MessageKey;
  const label = t(key);
  return label === key ? status : label;
}

/** Localized text for a form problem: field-specific first, then generic. */
export function problemText(p: { field: string; code: string; detail?: string } | null | undefined): string {
  if (!p) return "";
  for (const key of [`err.${p.field}.${p.code}`, `err.${p.code}`]) {
    const s = t(key as MessageKey, { detail: p.detail ?? "" });
    if (s !== key) return s;
  }
  return p.code;
}
