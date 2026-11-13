// Thin re-exports so components do not depend on the generated paths.
export * as App from "../../bindings/github.com/iwamatsu0430/sock-dock-slack/gui/appservice";
export type { Snapshot, Prefs, GatewayState, RouteForm, RouteSaveResult, FieldProblem, Channel, ChannelList, RuleList, RuleRow, ConnectResult, TokenProblem, MCPSetup } from "../../bindings/github.com/iwamatsu0430/sock-dock-slack/gui/models";
export type { Status, RunningTurn } from "../../bindings/github.com/iwamatsu0430/sock-dock-slack/internal/gateway/models";
export type { Turn } from "../../bindings/github.com/iwamatsu0430/sock-dock-slack/internal/store/models";
export { SockdockSnapshot, SockdockActivity, SockdockNavigate, SockdockLocale } from "../../bindings/events";

/** Turns an error from a bound call into a readable message. */
export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "string") return e;
  try {
    const o = e as { message?: string };
    if (o && typeof o.message === "string") return o.message;
    return JSON.stringify(e);
  } catch {
    return String(e);
  }
}
