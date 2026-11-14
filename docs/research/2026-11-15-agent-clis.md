# Codex CLI と Antigravity CLI（agy）の調査（2026-11-15）

ルールが動かすエージェントに、Claude Code のほか Codex CLI と Antigravity CLI（`agy`）を足せるかを調べた。公式の文書を読み、この Mac に入っている実物を動かして確かめた。

- 確かめた日: 2026-11-15
- 実物: `codex-cli 0.169.0`（ChatGPT でサインイン済み）、`agy 1.5.9`（Google アカウントでサインイン済み）。どちらも mise で入れたもの
- 試した場所: セッションのスクラッチパッド（`/private/tmp/...`）と、`/tmp` の影響を除くために `~/.cache` の下の一時フォルダ（試したあと消した）
- 前の調査: [`acp-spike.md`](acp-spike.md)（2026-11-13。ACP 経由。Codex と Gemini CLI は動かせなかった）

表の記号: ✓ = 実物で確かめた ／ 文 = 公式の文書だけ ／ ✗ = できない（実物で確かめた） ／ ？ = 分からない

## 1. Gemini CLI の終了と、Antigravity CLI

**ユーザーの言うとおり。ただし、終わったのは個人向けだけ。**

- 2026-05-19 に Google が発表した（Dmitry Lyalin、Taylor Mullen）。2026-06-18 に、Gemini CLI と Gemini Code Assist の IDE 拡張は、Google AI Pro / Ultra の契約者と、個人向けの無料の Gemini Code Assist からのリクエストに応えなくなった。
- Gemini CLI そのものは残る。Gemini Code Assist の Standard / Enterprise、Google Cloud 経由の Code Assist for GitHub、有料の Gemini API キー・Gemini Enterprise Agent Platform の API キーでは使える。
- 発表は、移る先を Antigravity CLI としている（記事の題が「Transitioning Gemini CLI to Antigravity CLI」）。「successor」という語は使っていない。最初は機能がそろわないとも書いている。
- Gemini CLI のリポジトリはアーカイブされていない。README に、個人向けは 2026-06-18 に Antigravity CLI へ移ったこと、企業向けには保守を続けることが書いてある。

出典:
- <https://developers.googleblog.com/en/an-important-update-transitioning-gemini-cli-to-antigravity-cli/>
- <https://github.com/google-gemini/gemini-cli>
- <https://antigravity.google/docs/cli/gcli-migration>

## 2. 結論

| | Claude Code（今） | Codex CLI | agy |
|---|---|---|---|
| ヘッドレスで動かし、最終回答を取る | ✓ | ✓ | ✓ |
| スレッドの返信を同じ会話で続ける | ✓ | ✓ | ✓ |
| Web 検索を止める | ✓ | ✓ | ✓（独自のエージェント定義を使ったときだけ） |
| ファイル編集を止める | ✓ | ✓（OS のサンドボックス） | ✓ |
| コマンドを許可したものに限る | ✓ | ✗（オフにもできない。オフにするとファイルを読めなくなる） | ✗（オンかオフだけ） |
| 作業フォルダの外を読ませない | ✓ | ✓（独自のプロファイルを使ったときだけ） | △（読み取りのツールは止まる。コマンドを許せばコマンドで読める） |
| MCP を止める | ✓ | ✓ | ✓（独自のエージェント定義を使ったときだけ） |
| 指示を足す | ✓ | ✓ | ✓（独自のエージェント定義の本文。既定の指示と入れ替わる） |
| サインインの確認 | ✓ `auth status` | ✓ `login status` | ？（確かめるコマンドがない） |
| 添付ファイル（画像） | ✓ | ✓ | ✓ |
| 個人のアカウントで SockDock から動かしてよいか | — | 問題は見つからなかった | **規約に触れるおそれ**（§4.9） |

- Codex は、Claude Code と同じ形で足せる。ただしコマンドは許可したものに限れない。いつもサンドボックスの中で動き、ネットワークは使えない。書き込めるのは作業フォルダの中だけで、それもファイル編集を許したときに限る。
- agy は技術的には足せる。ただ、コマンドを許可したものに限れないことに加えて、個人向けのプランでは、ほかのアプリから agy を動かすことを公式の FAQ がサポートの対象外とし、利用規約違反になりうるとしている。足すかどうかはユーザーが決める。

## 3. Codex CLI（`codex exec`）

公式の文書: <https://learn.chatgpt.com/docs/config-file/config-reference>（`developers.openai.com/codex/config-reference` からの転送先）、<https://developers.openai.com/codex/rules.md>、<https://developers.openai.com/codex/cli/reference>

### 3.1 起動と出力

| 項目 | 結果 |
|---|---|
| 起動 | `codex exec --json [オプション] -` ✓。`-` を付けると、プロンプトを標準入力から読む。引数を付けずにパイプしても読む |
| 出力 | `--json` で JSONL ✓。`{"type":"thread.started","thread_id":…}` → `turn.started` → `item.completed`（`agent_message` / `command_execution` / `web_search` など） → `turn.completed`（`usage`） |
| 最終回答 | そのターンの最後の `agent_message` の `text` ✓。`-o <file>` で最後の発言をファイルに書かせることもできる（文） |
| 失敗 | `turn.failed`（`error.message`）と exit 1 ✓ |
| 使用量 | `usage.input_tokens` / `cached_input_tokens` / `output_tokens` / `reasoning_output_tokens` ✓。**金額は出ない** |
| 作業フォルダ | 子プロセスの作業フォルダ、または `-C <dir>`。Git のリポジトリでない場所では `--skip-git-repo-check` が要る ✓ |

### 3.2 会話を続ける

- `thread.started` の `thread_id`（UUID）が会話の ID ✓。
- `codex exec resume <thread_id> --json -` で続けられた ✓。1 ターン目に答えたファイル名を、2 ターン目で覚えていた。
- 存在しない ID では、exit 1 で `Error: thread/resume: … no rollout found for thread id …` ✓。今の Claude Code と同じく、新しい会話でやり直せばよい。
- `exec resume` には `-s` / `--sandbox` がない ✓（ヘルプで確認）。許可の設定は、毎ターン `-c` で渡す。
- 保存先は `~/.codex` の SQLite（`state_5.sqlite` など）。残る期間は分からない（？）。`--ephemeral` を付けると残さない（文）。

### 3.3 許可

**サンドボックス**（`-s` / `sandbox_mode`）

| 値 | 実物での結果 |
|---|---|
| `read-only` | 作業フォルダの中にも外にも書けない ✓。**ディスク全体を読める**（ホームの下のファイルを `cat` で読めた ✓） |
| `workspace-write` | 作業フォルダの中だけ書ける（文。2026-11-15 の試験は途中で止めたため、作業フォルダに `made.txt` ができたことだけ確かめた） |
| `danger-full-access` | 使わない |

- **承認**: `approval_policy = "never"` で、承認を求めずに進む。サンドボックスに止められた操作は失敗としてモデルに返る（文。ヘルプにも「Execution failures are immediately returned to the model」とある）。`codex exec` の既定でも、承認を待って止まることはなかった ✓。
- **コマンド**: サンドボックスの中のコマンドは、1 つずつ許可を求めずに動く。規則（`.rules` の `prefix_rule`）の既定は「許可」で、`prompt` / `forbidden` を書いたコマンドだけを止められる。どのコマンドにも当たる規則は書けない。そのため「許可したコマンドだけ」は作れない（文）。
- **コマンドを止める**: `--disable shell_tool --disable unified_exec` でコマンドのツールが消えた ✓。ただし Codex はファイルをコマンドで読むので、作業フォルダも読めなくなる。実用にならない。
- **フォルダの外を読ませない**: 独自の権限プロファイルで止められた ✓。
  ```
  -c default_permissions="sd"
  -c 'permissions.sd.filesystem={":minimal"="read", "<codex のインストール先>"="read", ":workspace_roots"={"."="read"}}'
  ```
  ホームの下のファイルは `Operation not permitted` になり、作業フォルダは読めた。codex 自身のインストール先（mise なら `~/.local/share/mise/installs/codex`）を読めるようにしないと起動しない ✓。`"."="write"` にすれば、フォルダの中に書ける（文）。`default_permissions` と `sandbox_mode` は一緒に使えない（文）。
- **Web 検索**: 既定は `web_search = "cached"`（OpenAI の索引を使い、外へは取りに行かない）で、**何もしなければ検索する** ✓。`-c web_search="disabled"` で検索のツールが消えた ✓。許可するときは `"live"`（文）。コマンドのネットワークは、サンドボックスが塞ぐ（文）。
- **MCP**: `--ignore-user-config` で、利用者の `~/.codex/config.toml` の MCP サーバーとプラグインが消えた ✓。サインインは `CODEX_HOME` のまま使える。ChatGPT のアプリ（`mcp__codex_apps__*`）は残るので、`--disable apps` で消す ✓。`--ignore-rules` で、利用者とプロジェクトの `.rules` を読まない（文）。

### 3.4 指示・モデル・作業フォルダの設定

- **指示を足す**: `-c developer_instructions="…"` で効いた ✓（返事の終わりに目印を付けさせた）。ファイルからは読めない。指示の文を TOML の文字列にして渡す。`model_instructions_file` は、既定の指示と**入れ替える**ものなので使わない（文）。
- **モデル**: `-m <model>` ✓（ヘルプ）。
- **作業フォルダから読むもの**: `AGENTS.md`（作業フォルダから上へリポジトリのルートまで）（文）。`.codex/config.toml`、フック、`.rules` は、プロジェクトを信頼したときだけ読む（文）。`--trust` は付けない。

### 3.5 サインイン

- 方法: ChatGPT のアカウント（`codex login`）、API キー（`codex login --with-api-key`）、アクセストークン。保存先は `CODEX_HOME`（既定 `~/.codex`）の `auth.json`（文）。
- 確認: `codex login status` → サインイン済みなら exit 0 と `Logged in using ChatGPT`、未サインインなら exit 1 と `Not logged in` ✓（空の `CODEX_HOME` で確かめた）。
- 未サインインで `exec` すると、`401 Unauthorized` で 10 回ほどつなぎ直してから `turn.failed`、exit 1 になる ✓。約 20 秒かかる。トークンの期限切れのときの文言は確かめていない（？）。
- 自動化での使い方: 公式の文書は、自動化には API キーを勧めつつ、ChatGPT のアカウントのサインイン（`~/.codex/auth.json`）を使い回すことも「信頼できる自分のマシン」向けに認めている。agy のような禁止の記述は見つからなかった。出典: <https://developers.openai.com/codex/noninteractive>、<https://developers.openai.com/codex/auth/ci-cd-auth>

### 3.6 添付ファイル

- `-i <file>` で画像を付けられた ✓（色を当てた）。`exec resume` にも `-i` がある。
- プロンプトにパスを書いても、自分で画像を開いて読んだ ✓。権限プロファイルで読める場所を絞ったときは、添付ファイルのフォルダを `"read"` に足す。

## 4. Antigravity CLI（`agy`）

公式の文書: <https://antigravity.google/docs/cli/headless>、<https://antigravity.google/docs/cli/reference>、<https://antigravity.google/docs/permissions>、<https://antigravity.google/docs/settings>、<https://antigravity.google/docs/subagents>、<https://antigravity.google/docs/rules>、<https://antigravity.google/docs/install>、<https://antigravity.google/docs/faq>、CHANGELOG <https://github.com/google-antigravity/antigravity-cli>。どのページにも更新日はない。

### 4.1 起動と出力

| 項目 | 結果 |
|---|---|
| 起動 | `agy -p <プロンプト>` は、プロンプトを**引数でしか受け取らない** ✓。`-p -` は「-」をプロンプトとして送った ✓ |
| 標準入力で渡す | `--input-format stream-json --output-format stream-json -p=` で、1 行に 1 つの `{"event":"user","message":{"content":"…"}}` を送る ✓。標準入力を閉じると、そのターンが終わってから exit 0 で終わる ✓ |
| 出力 | `init`（`cwd`、`tools`、`permission_mode`） → `step_update`（ツールの実行、回答の差分） → `result` ✓ |
| 最終回答 | `result.response` ✓。ほかに `conversation_id`、`status`（SUCCESS / ERROR など）、`error`、`num_turns`、`usage`、`denied_actions`、`finished_reason` |
| 使用量 | `input_tokens` / `output_tokens` / `thinking_tokens` / `cache_read_tokens` / `total_tokens` ✓。**金額は出ない** |
| exit code | 0 成功（拒否があっても 0）、1 引数やモデルの誤り、2 使い方の誤り、3 エージェントや API の失敗（文、CHANGELOG） |
| 時間の上限 | `--print-timeout`（既定は無制限） |
| TTY のない環境 | パイプから動かして、待ち続けることはなかった ✓（1.5.9。以前の版にはハングする不具合の報告があった: <https://github.com/google-antigravity/antigravity-cli/issues/318>） |

### 4.2 会話を続ける

- `--conversation <conversation_id>` で続けられた ✓。1 ターン目に答えたファイル名を覚えていた。
- **存在しない ID でも失敗しない** ✓。新しい会話を別の ID で始め、標準エラーに `warning: conversation "…" not found` を出して exit 0。返ってきた `conversation_id` が渡した ID と違えば、再開できなかったと分かる。
- 会話は `~/.gemini/antigravity-cli/conversations/<id>.db` に残る ✓。残る期間は分からない（？）。

### 4.3 許可

**既定（`toolPermission: request-review`、ヘッドレス）の実物での結果**:

| 操作 | 結果 |
|---|---|
| 作業フォルダの中への書き込み | 確認なしで書いた ✓ |
| コマンド（`touch`、フォルダの外のファイルを `cat`） | **確認なしで動いた** ✓。文書の「書いていない操作は確認する」と食い違う |
| Web 検索 | 確認なしで検索した ✓ |
| 読み取りのツールで、作業フォルダの外を読む | 拒否された ✓ |

**拒否されたとき**: 確認の要る操作が 1 つでも出ると、**そのターンは回答なしで終わる** ✓。`response` が空になり、`denied_actions` に拒否された種類が入る。標準エラーには `no output produced — a tool required the "…" permission …` が出て、exit 0。モデルは拒否されたことを返事に書けない。

**`--settings`（その回だけの設定。保存しない）で試したこと**:

| 設定 | 結果 |
|---|---|
| `permissions.deny: ["write_file(*)"]` | 書き込みが拒否され、モデルがそれを返事に書いた ✓（ターンは続く） |
| `permissions.deny: ["command(*)"]` | コマンドが拒否され、モデルがそれを返事に書いた ✓ |
| `permissions.ask: ["command(touch)"]` | `touch` だけが止まり、`echo` は動いた ✓（前方一致が効く） |
| `permissions.deny: ["read_url(*)"]` や `["search_web(*)"]` | **Web 検索は止まらなかった** ✓。`search_web` は規則の種類にない（文） |
| `ask: ["command(*)"]` ＋ `allow: ["command(echo)"]` | `echo` も止まった ✓（ask が allow より強い） |
| `toolPermission: "strict"` ＋ `allow: ["command(echo)"]`（`command(echo *)`、`command(regex:echo.*)` も） | `echo` も止まった ✓ |
| `toolPermission: "strict"` ＋ `allow: ["command(*)"]` | 動いた ✓ |

→ **コマンドを許可したものに限ることはできない**（1.5.9）。オンかオフだけ。拒否の規則は前方一致で効くので、「このコマンドは止める」なら書ける。

**独自のエージェント定義**（`.agents/agents/<name>.md` の YAML の前書き ＋ 本文）:

```markdown
---
name: sdtest
description: SockDock test agent
tools: ["view_file", "list_dir", "grep_search"]
mainAgent: true
subagent: false
---
（本文がシステムプロンプトになる）
```

- `--agent sdtest` で選ぶと、`tools` にないツールは使えなかった ✓。Web 検索もコマンドも使えず、モデルはそう答えた。
- 本文の指示が効いた ✓。入力のトークンが約 9,500 から約 2,300 に減ったので、**既定のシステムプロンプトと入れ替わる**と見られる。
- `--agent` にファイルのパスは渡せない ✓（`unknown agent`）。置き場所は、作業フォルダの `.agents/agents/`、`~/.gemini/config/agents/`、プラグインの中（文）。**`--add-dir <フォルダ>` で足したフォルダの `.agents/agents/` からも読めた** ✓。SockDock は自分のデータフォルダに定義を書き、`--add-dir` で渡せば、利用者のフォルダも設定も書き換えずに済む。
- `tools` の名前を間違えると、ハングすることがある（文。既知の不具合）。
- ツールの名前（`init.tools` より ✓）: 読む `view_file` `list_dir` `grep_search` `find_by_name` ／ 書く `write_to_file` `replace_file_content` `multi_replace_file_content` `sed_file` `notebook_edit` ／ コマンド `run_command` `command_status` `send_command_input` ／ Web `search_web` `read_url_content` ／ MCP `call_mcp_tool` `list_resources` `read_resource` ／ ブラウザ `browser_*` `open_browser_url` など ／ ほかに `generate_image` `schedule` `send_message` `invoke_subagent` など。

**そのほか**:
- `allowNonWorkspaceAccess`（既定オフ）で、作業フォルダの外への読み書きを開く（文）。
- サンドボックス: `--sandbox` か `enableTerminalSandbox`。macOS では Seatbelt で、ネットワークは既定で塞ぐ（文）。`--settings` で `"enableTerminalSandbox":"on"` は値の誤りになった ✓（正しい値は確かめていない）。
- 利用者の `~/.gemini/antigravity-cli/settings.json` の allow の規則も効く（文）。拒否は allow より強いので、SockDock の deny は効く。

### 4.4 指示・モデル

- **指示を足す**: フラグも環境変数もない（文）。独自のエージェント定義の本文だけ ✓（既定の指示と入れ替わる）。
- **モデル**: `--model <slug>`。slug は `agy models` で出る ✓（Gemini のほか、Claude と GPT-OSS もあった）。`--effort low|medium|high|xhigh|max`。

### 4.5 作業フォルダから読むもの

- `AGENTS.md` / `GEMINI.md`、`.agents/AGENTS.md`、`.agents/rules/*.md`（上へリポジトリのルートまで）（文）。
- `.agents/mcp_config.json`、`.agents/hooks.json`、`.agents/plugins/`、`.agents/skills/`、`.agents/agents/`（文）。
- **信頼の確認**: 信頼していないフォルダ（`trustedWorkspaces` にない）でも、ヘッドレスでは聞かずに動いた ✓。そのフォルダの `.agents/agents/` も読んだ ✓。フックは信頼したフォルダだけで読む（文、1.1.1）。
- 読ませない方法は書かれていない（？）。

### 4.6 サインイン

- 方法: Google アカウント（ブラウザ）、Gemini API キー（`"modelProvider":"gemini"` と `GEMINI_API_KEY`）、Business / Vertex、ADC（`AGY_ADC_AUTH=true`）（文）。資格情報は macOS のキーチェーン（文）。Antigravity のアプリ・IDE と共通で、どれかでサインインすれば CLI も使える（文: 「Signing in on one local surface shares your credentials across your local environment」）。
- ヘッドレスの前に、一度対話でサインインしておく。していなければ `authentication required` で終わる（文）。期限切れは `Authentication expired`、キーチェーンが開いていなければ `secret keyring is locked`（文、CHANGELOG と troubleshooting）。
- **サインインを確かめるコマンドはない** ✓（`agy --help` に `auth` も `login` もない）。サインインは `agy` を対話で開いて行う。`agy models` はサーバーからモデルを取るので、確かめる代わりに使えるかもしれない（未サインインのときの動きは確かめていない（？））。

### 4.7 添付ファイル

- ヘッドレスで添付を渡すフラグはない（文）。stream-json の入力も text だけ ✓。
- プロンプトに画像のパスを書き、そのフォルダを `--add-dir` で足すと、画像を読んで色を当てた ✓。`--add-dir` で足したフォルダは書き込みもできるようになるので、`write_file(<そのフォルダ>)` を拒否に入れる。

### 4.8 ACP

- `agy acp` で ACP のサーバーとして動く（1.5.0 から）。`session/resume` にも応じる（文）。

### 4.9 利用規約（重要）

公式の FAQ（<https://antigravity.google/docs/faq>）:

- ヘッドレス（`agy -p`）は「direct scripting and CI workflows」向け。
- 個人向けのプランでは、ほかのコーディングエージェントやアプリの中で agy を包むことはサポートしない。
- 「Using third-party software, tools, or services to access Antigravity is a violation of our Terms of Service」。アカウントの停止や解約もありうる。
- ほかのコーディングエージェントで Gemini を使うなら、Gemini Enterprise か Google AI Studio の API キーを勧めている。

SockDock は「利用者の Mac で、利用者が書いたルールに従って agy を起動するもの」で、利用者自身のスクリプトに近い。それでも「ほかのアプリで agy を包む」に当たると読める。公式フォーラムには「サポートされている」という回答もあるが、Google の社員の印がなく、FAQ と食い違ったままになっている（<https://discuss.ai.google.dev/t/is-external-orchestration-of-antigravity-cli-headless-mode-supported-with-account-based-usage/183051>）。API キー（`modelProvider: gemini`）か Enterprise で使うなら当たらない。

## 5. この調査で残したもの

- Codex の会話: 試した 10 件ほどが `~/.codex` に残っている（`codex exec` は既定で会話を残すため）。
- agy の会話: 試した 30 件ほどが `~/.gemini/antigravity-cli/conversations/` に残っている。
- 利用者の設定ファイル（`~/.codex/config.toml`、`~/.gemini/antigravity-cli/settings.json`）は書き換えていない。

## 6. 分からないこと

1. Codex のトークンの期限が切れたときの文言
2. agy のサインインを確かめる方法（`agy models` が未サインインでどう終わるか）
3. 未サインインのときの agy の exit code
4. agy で `enableTerminalSandbox` を `--settings` で入れるときの値
5. agy で、作業フォルダの `AGENTS.md` や `.agents/` を読ませない方法
6. 会話がどのくらい残るか（Codex、agy とも）
7. agy の `--settings` が、利用者の `settings.json` の `permissions` に足されるのか、置き換えるのか
