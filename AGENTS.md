# AGENTS.md

SockDock for Slack: a Mac menu bar app (Go + Wails v3 + Svelte) that runs rules for Slack posts. See README.md for what it does, the layout and the mise tasks.

## Checks

- `mise run ci` before committing (vet + gofmt, tests with -race, frontend type check).
- After changing Go types or methods the GUI uses, run `mise run bindings`. Never edit `gui/frontend/bindings/` by hand.
- Try the GUI with `mise run preview` (fake Slack, sample rules). Never touch the real config or Keychain while developing; `SOCKDOCK_DATA_DIR` / `SOCKDOCK_CONFIG` keep them apart.

## Working notes

- Specs, IA, mocks and research go in `.claude/tmp/` (gitignored). Don't add them to the repo, and don't point at them from code comments or commit messages: they are not public.
- UI changes start from the IA (`.claude/tmp/ia.md`): for each element, the reader's question, what it tells, and why. Then a mock, then the user's approval, then code. Answer "unclear" by cutting, not by adding text.

## Copy

- UI strings live in `gui/frontend/src/lib/i18n/{en,ja}.ts` and `gui/i18n.go`. Write each language natively; never translate one from the other.
- Japanese: sentences in です・ます, headings and buttons as nouns (「Slack に接続」). Reread for translationese.
- The LP is `site/` (`index.html`, `ja/index.html`), published to GitHub Pages on push to `main`.

## Commits and releases

- Conventional prefixes: `feat:`, `fix:`, `docs:`, `chore:`; one change per commit.
- Release with the `release` skill (`.claude/skills/release/`). The notes go in the annotated tag message, not in a file.
