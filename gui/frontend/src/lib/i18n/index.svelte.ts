import { Events } from "@wailsio/runtime";
import { App, SockdockLocale } from "../api";
import { en, type MessageKey } from "./en";
import { ja } from "./ja";

const dictionaries: Record<string, Record<MessageKey, string>> = { en, ja };

/** Native names shown in the language picker. */
export const languageNames: Record<string, string> = { en: "English", ja: "日本語" };

export const i18n = $state({ locale: "en" });

let started = false;

function apply(locale: string) {
  i18n.locale = dictionaries[locale] ? locale : "en";
  document.documentElement.lang = i18n.locale;
}

/** Loads the UI language from the app and follows changes (idempotent). */
export function startI18n() {
  if (started) return;
  started = true;
  App.Locale().then(apply);
  Events.On(SockdockLocale, (ev) => apply(ev.data as string));
}

/** Sets the language immediately in this window (other windows get the event). */
export function setLocale(locale: string) {
  apply(locale);
}

type Params = Record<string, string | number>;

/** Translates a key, filling {params}. Reading i18n.locale makes it reactive. */
export function t(key: MessageKey, params?: Params): string {
  const dict = dictionaries[i18n.locale] ?? en;
  let s: string = dict[key] ?? en[key] ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) s = s.replaceAll(`{${k}}`, String(v));
  }
  return s;
}

export type Segment = { kind: "text" | "code" | "em" | "slot"; text: string };

/**
 * Translates a key into segments for `code`, *emphasis* and {slot}
 * placeholders (slots not given in params stay as "slot" segments so the
 * caller can render a link or button there). No HTML is ever interpreted.
 */
export function rich(key: MessageKey, params?: Params): Segment[] {
  const dict = dictionaries[i18n.locale] ?? en;
  const src: string = dict[key] ?? en[key] ?? key;
  const out: Segment[] = [];
  const re = /`([^`]+)`|\*([^*]+)\*|\{(\w+)\}/g;
  let last = 0;
  for (let m = re.exec(src); m; m = re.exec(src)) {
    if (m.index > last) out.push({ kind: "text", text: src.slice(last, m.index) });
    if (m[1] !== undefined) out.push({ kind: "code", text: m[1].replace(/\{(\w+)\}/g, (x, k) => (params && k in params ? String(params[k]) : x)) });
    else if (m[2] !== undefined) out.push({ kind: "em", text: m[2] });
    else if (params && m[3] in params) out.push({ kind: "text", text: String(params[m[3]]) });
    else out.push({ kind: "slot", text: m[3] });
    last = re.lastIndex;
  }
  if (last < src.length) out.push({ kind: "text", text: src.slice(last) });
  return out;
}
