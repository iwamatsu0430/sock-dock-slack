import { i18n, t } from "./i18n/index.svelte";
import type { MessageKey } from "./i18n/en";

export function ago(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return "—";
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (s < 60) return t("time.secondsAgo", { n: s });
  if (s < 3600) return t("time.minutesAgo", { n: Math.floor(s / 60) });
  if (s < 86400) return t("time.hoursAgo", { n: Math.floor(s / 3600) });
  return t("time.daysAgo", { n: Math.floor(s / 86400) });
}

function span(s: number): string {
  s = Math.max(0, Math.round(s));
  return s < 60 ? t("time.seconds", { s }) : t("time.minutesSeconds", { m: Math.floor(s / 60), s: s % 60 });
}

export function elapsed(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return "";
  return span((now - new Date(iso).getTime()) / 1000);
}

export function duration(start: string, end: string | null | undefined): string {
  if (!end) return "—";
  return span((new Date(end).getTime() - new Date(start).getTime()) / 1000);
}

export function clock(iso: string): string {
  return new Date(iso).toLocaleString(i18n.locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function statusLevel(status: string): "ok" | "warn" | "bad" | "" {
  switch (status) {
    case "ok":
      return "ok";
    case "running":
      return "";
    case "timeout":
    case "interrupted":
      return "warn";
    default:
      return "bad";
  }
}

/** Localized label for a turn status. */
export function statusLabel(status: string): string {
  const key = `status.${status}` as MessageKey;
  const label = t(key);
  return label === key ? status : label;
}
