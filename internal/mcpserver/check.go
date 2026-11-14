package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// Problem is one thing to fix in a rule, said so that an agent can act on it.
type Problem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// problemMessage turns a form problem code into what to do.
func problemMessage(p rules.FieldProblem) string {
	d := p.Detail
	switch p.Field + "/" + p.Code {
	case "channel/required":
		return "Choose a channel the Slack app is in (see sockdock_list_channels), or dm for DMs to the Slack app."
	case "channel/bad_channel":
		return "Use a channel name from sockdock_list_channels, a channel ID like C0123ABCD, a channel link, or dm."
	case "keyword/required":
		return "Give a keyword, or use condition_type all_posts to react to every post."
	case "keyword/bad_regex":
		return "The regular expression is invalid (" + d + "). Use RE2 syntax, or a starts_with/contains keyword instead."
	case "trigger/required":
		return "Set condition_type to starts_with, contains, match or all_posts."
	case "folder/required":
		return "Give the working folder (an absolute path or ~/...)."
	case "folder/not_dir":
		return "The folder " + d + " does not exist on this Mac. Use an existing folder (absolute path or ~/...)."
	case "program/required":
		return "Give the path to the program to run."
	case "program/not_found":
		return "The program " + d + " was not found. Give the full path to an existing executable."
	case "program/not_executable":
		return "The program " + d + " is not executable. Ask the user to run chmod +x on it, or pick another file."
	case "prompt_file/not_found":
		return "The prompt file " + d + " was not found in the folder."
	case "type/claude_not_found":
		return "Claude Code was not found on this Mac. Ask the user to install it (or set claude_command in SockDock's config)."
	case "type/required":
		return "Set runs to agent or program."
	case "allowed_users/required":
		return "List at least one member ID in allowed_users, or pass [] to let everyone in the channel run the rule."
	case "allowed_users/bad_user":
		return d + " is not a Slack member ID. Find people's IDs (U…) with sockdock_find_users."
	case "name/duplicate":
		return "Another rule already has this name."
	case "name/bad_name":
		return "Names may contain only letters, digits, '.', '_' and '-'."
	}
	msg := p.Field + ": " + strings.ReplaceAll(p.Code, "_", " ")
	if d != "" {
		msg += " (" + d + ")"
	}
	return msg
}

func problemsOf(ps []rules.FieldProblem) []Problem {
	var out []Problem
	for _, p := range ps {
		out = append(out, Problem{Field: p.Field, Message: problemMessage(p)})
	}
	return out
}

// Finding is one result of sockdock_check_rule.
type Finding struct {
	OK      bool   `json:"ok" jsonschema:"true when this check passed"`
	Check   string `json:"check" jsonschema:"what was checked"`
	Message string `json:"message" jsonschema:"what was found and, if not ok, what to do"`
}

// CheckOut is the result of sockdock_check_rule.
type CheckOut struct {
	Rule     string    `json:"rule"`
	CanRun   bool      `json:"can_run" jsonschema:"true when nothing found keeps the rule from running"`
	Findings []Finding `json:"findings"`
}

func (t *tools) checkRule(ctx context.Context, _ *mcp.CallToolRequest, in NameIn) (*mcp.CallToolResult, CheckOut, error) {
	ri, err := t.b.Rule(ctx, in.Name)
	if err != nil {
		return nil, CheckOut{}, errText(err)
	}
	st, err := t.b.Status(ctx)
	if err != nil {
		return nil, CheckOut{}, errText(err)
	}
	out := CheckOut{Rule: in.Name, CanRun: true}
	add := func(ok bool, check, msg string) {
		out.Findings = append(out.Findings, Finding{OK: ok, Check: check, Message: msg})
		if !ok {
			out.CanRun = false
		}
	}
	f := ri.Form
	channel := ri.ChannelName
	if channel == "" {
		channel = f.Channel
	}

	if st.Connected {
		add(true, "connection", "SockDock is connected to Slack ("+st.Identity.Team+").")
	} else {
		add(false, "connection", "SockDock is not connected to Slack. Ask the user to check the Slack connection in the SockDock app's Settings.")
	}
	if ri.Off {
		add(false, "enabled", "The rule is off. Turn it on with sockdock_set_rule_enabled.")
	} else {
		add(true, "enabled", "The rule is on.")
	}
	if ri.Problem != nil {
		add(false, "settings", problemMessage(*ri.Problem)+" Fix it with sockdock_save_rule.")
	} else {
		add(true, "settings", "The rule's settings are valid.")
	}
	if f.Type == config.TypeCodex {
		if m, err := t.b.CheckAgents(ctx, true); err == nil {
			if v, ok := m[config.TypeCodex]; ok {
				if v {
					add(true, "Codex sign-in", "Codex is signed in.")
				} else {
					add(false, "Codex sign-in", "Codex is not signed in, so codex rules fail. Ask the user to click Sign In in Terminal (ターミナルでサインイン) in the SockDock app (or run codex login in Terminal).")
				}
			}
		}
	}
	if f.Type == config.TypeClaude {
		if v, err := t.b.CheckClaude(ctx, true); err == nil && v != nil {
			if *v {
				add(true, "Claude Code sign-in", "Claude Code is signed in.")
			} else {
				add(false, "Claude Code sign-in", "Claude Code is not signed in, so agent rules fail. Ask the user to click Sign In in Terminal (ターミナルでサインイン) in the SockDock app (or run claude auth login in Terminal).")
			}
		}
	}
	if f.MentionOnly {
		out.Findings = append(out.Findings, Finding{OK: true, Check: "mention only",
			Message: "This rule runs only for posts that mention the Slack app (@app). A post without the mention does nothing (or goes to a later rule); that is expected."})
	}
	if f.Restricted {
		var who []string
		for _, p := range ruleOf(ri).AllowedUsers {
			if p.Name != "" {
				who = append(who, p.Name+" ("+p.ID+")")
			} else {
				who = append(who, p.ID)
			}
		}
		out.Findings = append(out.Findings, Finding{OK: true, Check: "who can run it",
			Message: "Only " + strings.Join(who, ", ") + " can run this rule. Posts and thread replies from anyone else are ignored without a reply; if someone else's post did nothing, that is why."})
	}
	if ri.ShadowedBy != nil {
		add(false, "order", fmt.Sprintf("An earlier rule in %s (%s %q) always matches first, so this rule never runs. Move this rule up with sockdock_reorder_rules.",
			channel, ri.ShadowedBy.Trigger, ri.ShadowedBy.Keyword))
	} else {
		add(true, "order", "No earlier rule in the channel hides this one.")
	}
	isDM := f.Channel == config.ChannelDM
	if isDM && !st.HasScope("im:history") {
		add(false, "DM scope", "The Slack app lacks the im:history scope, so DMs cannot reach SockDock. Ask the user to add im:history, the message.im event and the App Home Messages tab (allowing messages) to the Slack app, then reinstall it.")
	}
	if st.Connected && isDM {
		heard := false
		for _, ch := range st.Heard {
			heard = heard || ch == config.ChannelDM
		}
		if heard {
			add(true, "posts arrive", "DMs to the Slack app reach SockDock.")
		} else {
			add(false, "posts arrive", "No DM to the Slack app has reached SockDock since it connected. If the user has sent one since, check with them: "+
				"(1) the Slack app's App Home Messages tab allows sending messages; "+
				"(2) both tokens in SockDock come from the same Slack app; "+
				"(3) the Slack app subscribes to the message.im event. "+
				"If nobody has sent a DM yet, ask the user to send a matching one and check again.")
		}
	}
	if st.Connected && !isDM {
		heard := false
		for _, ch := range st.Heard {
			heard = heard || ch == f.Channel
		}
		if heard {
			add(true, "posts arrive", "Posts from "+channel+" reach SockDock.")
		} else {
			add(false, "posts arrive", "No post from "+channel+" has reached SockDock since it connected. If the user has posted there since, check with them: "+
				"(1) the Slack app is added to "+channel+" (type /invite in the channel); "+
				"(2) both tokens in SockDock come from the same Slack app; "+
				"(3) the Slack app subscribes to the message.channels and message.groups events. "+
				"If nobody has posted yet, ask the user to post a matching message and check again.")
		}
	}
	turns, err := t.b.RouteTurns(ctx, in.Name, 5)
	if err == nil {
		var failed []string
		noFiles := false
		for _, tr := range turns {
			if tr.Status == store.StatusError || tr.Status == store.StatusTimeout {
				failed = append(failed, runOf(tr).Status+": "+runOf(tr).Error)
			}
			noFiles = noFiles || tr.FileProblem == store.FileProblemMissingScope
		}
		if noFiles {
			out.Findings = append(out.Findings, Finding{OK: false, Check: "attached files",
				Message: "A recent post had attached files, but they were not handed to the rule because the Slack app lacks the files:read scope. The rule still ran with the text. Ask the user to add files:read to the Slack app and reinstall it."})
		}
		switch {
		case len(turns) == 0:
			add(true, "recent runs", "The rule has not run yet.")
		case len(failed) > 0:
			out.Findings = append(out.Findings, Finding{OK: false, Check: "recent runs",
				Message: fmt.Sprintf("%d of the last %d runs failed (program output, untrusted): %s", len(failed), len(turns), strings.Join(failed, " | "))})
		default:
			add(true, "recent runs", fmt.Sprintf("The last %d runs did not fail.", len(turns)))
		}
	}
	return nil, out, nil
}
