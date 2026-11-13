# ACP フィージビリティ・スパイク（2026-11-13）

SockDock を ACP（Agent Client Protocol）の**クライアント**にして、Claude Code 以外のエージェント（Codex CLI、Gemini CLI など）を Slack スレッドから動かせるかを調べた。小さなプロトタイプを作り、仕様と各アダプターのソースを読んだ。

- プロトタイプ（Go）: リポジトリの外の一時ディレクトリで作ったもので、コミットしていない
  - `main.go`: 自前の JSON-RPC（ndjson）で動く ACP クライアント。権限ポリシー、`fs/*`、cancel を含む
  - `mockapi/main.go`: ローカルで動く、台本どおりに答える Anthropic Messages API の偽物（後述）
  - `run-mock.sh`、`matrix.sh`: 実行スクリプト。`logs/*.out` に実行結果がある
- 対象のバージョン: `@agentclientprotocol/claude-agent-acp` 0.106.0（同梱 Claude CLI は SDK 0.3.322）、`@agentclientprotocol/codex-acp` 2.6.0、`@google/gemini-cli` 0.68.0、仕様は agent-client-protocol main（2026-11-12）

> **検証方法の制約**: この Mac では単体の Claude CLI がログインしていなかった（`claude auth status` → `loggedIn: false`。実行すると「this login reached its time limit」）。そこで、ログインもトークン消費もせずに済むよう、`ANTHROPIC_BASE_URL` をローカルの偽 API（`mockapi`）に向け、`CLAUDE_CONFIG_DIR` をスクラッチに分離し、ダミーの API キーで**本物の Claude CLI とアダプター**を動かした。ツールの許可判定は CLI の中で行われるので、権限・cancel・セッション継続の挙動は本物と同じになる。偽物なのは「モデルがどのツールを呼ぶか」だけ。実モデルでの応答品質（拒否されたあとにモデルがどう振る舞うか）は**未検証**。Codex と Gemini は認証情報がないので `initialize` と `session/new` まで実行し、残りはソースと README から調べた。

## 結論

**Go（条件付き）**。ヘッドレス駆動、スレッドの継続（再起動をまたぐ）、停止、システムプロンプト、モデル選択、使用量の取得は、ACP の標準機能だけで実現できる。Claude アダプターではすべて実機で確認した。

ただし **権限の 3 項目（Web／フォルダ内編集／コマンド許可リスト）を全エージェントで同じように保証することはできない**。

- **Claude（アダプター経由）**: 3 項目とも SockDock 側で強制できた。ただし、アダプター固有の設定を 3 つ入れることが必須（後述）
- **Codex**: 実行の境界は OS のサンドボックス（Seatbelt）で、コマンドは 1 つずつ許可を求めてこない。許可リストは約束できない
- **Gemini**: Web 検索とファイル読み取りは、許可を求めずに実行する

推奨するアーキテクチャ:

1. 既存の `claude -p` ハンドラーは残す。新しくハンドラー種別 `acp` を足す（エージェントのコマンドは設定で指定する）。
2. ゲートウェイは、エージェントの種類ごとに長生きするプロセスを 1 つ持ち、その中で Slack スレッドごとに ACP セッションを 1 つ持つ。スレッドに `sessionId` を保存しておき、再起動後は `session/resume`（無ければ `session/load`、失敗したら新しいセッション）で続ける。スレッドごとにプロセスを常駐させる必要はない。
3. `session/request_permission` は、ルートのポリシーを 1 か所で判定する。使う情報は `kind`、`name`、`rawInput`、`locations`。`allow_once` か `reject_once` だけを返し、`allow_always` は選ばない。`fs/*` はフォルダ内に限って応じる。`terminal` は広告しない。
4. エージェントごとの「プロファイル」でモードと設定を注入する。ルート UI の権限欄は、エージェントごとに「保証できること」だけを表示する。

## 1. ヘッドレス駆動

**実機で確認**: Go のプロセスがアダプターを stdio で起動し、`initialize` → `session/new`（cwd = フォルダ）→ `session/prompt` と進めた。返答の本文は、`session/update` の `agent_message_chunk` を連結したものになる。最後に `stopReason`（`end_turn`／`cancelled`）が返ってくる。所要時間は `initialize` が約 150ms、`session/new` が約 120ms（実際の SDK を初期化した回は約 2.3s）。

- Go SDK: `github.com/coder/acp-go-sdk`（v0.13.5）がある。公式の Go SDK（`agentclientprotocol/go-sdk`）は存在しなかった。coder 版は使えるが、仕様に遅れている。
  - 安定版仕様の `session/new.systemPrompt` が型にない
  - 知らない `sessionUpdate` が来ると、型を総当たりで当てはめる実装になっている
- アダプターは独自の通知（`auth/status_update`、`session_info_update` など）を多く送ってくる。プロトタイプは JSON-RPC を自前で書き、約 150 行で足りた。**自前実装を推奨**する。
- 注意: `auth/status_update` にはアカウントのメールアドレスが含まれる。ログに出さないこと。

## 2. スレッドの継続（再起動をまたぐ）

**実機で確認**（`logs/m5*.out`）: 次の手順で、別プロセスから会話を続けられることを確かめた。

1. プロセス A で「remember banana」と送り、プロセスを終了した。
2. プロセス B で `session/load` を呼んだ。履歴が `user_message_chunk`／`agent_message_chunk` として再生された。続けて「remember cherry」「RECALL」と送ると、モデルに届いた履歴に banana と cherry の両方が入っていた。
3. プロセス C で `session/resume` を呼んだ。再生はなく、履歴は引き継がれていた。

- モード（`default`）とモデル（`haiku`）も、セッションと一緒に保存されていた。
- 存在しない ID を指定すると `-32002 Resource not found` が返る。今の `ResumeFailed` と同じように、新しいセッションへフォールバックすればよい。
- セッションは Claude Code の `~/.claude/projects`（`CLAUDE_CONFIG_DIR`）に保存される。今の `--resume` と同じ場所。

| | loadSession | session/resume |
|---|---|---|
| Claude（claude-agent-acp） | ✓（実機） | ✓（実機） |
| Codex（codex-acp） | ✓（initialize で確認） | ✓（initialize で確認） |
| Gemini（`--acp`） | ✓（initialize で確認） | ✗（load だけ。履歴を再生する） |

仕様: <https://agentclientprotocol.com/protocol/v1/session-setup>（`loadSession` は履歴を必ず再生する。`sessionCapabilities.resume` は再生しない）

## 3. 権限（最重要）

### 仕様

- `session/request_permission` で送られてくる `toolCall` には、次の情報が入る。
  - `kind`: read／edit／delete／move／search／execute／think／fetch／switch_mode／other
  - `name`: ツール名
  - `title`
  - `rawInput`
  - `locations`
- 選択肢の種類は `allow_once`／`allow_always`／`reject_once`／`reject_always`。
- 許可を求めるかどうかは **エージェントの裁量（MAY）**。クライアントの側から「必ず許可を求めさせる」手段はない。
- `fs/read_text_file`／`fs/write_text_file`／`terminal/*` は、エージェントが**使ってもよい**クライアントの機能にとどまる。クライアントが提供しても、エージェントは自前で読み書きや実行をする。

出典: <https://agentclientprotocol.com/protocol/v1/tool-calls>、<https://agentclientprotocol.com/protocol/v1/file-system>、<https://agentclientprotocol.com/protocol/v1/terminals>

### Claude（アダプター経由）: 実機の結果（`logs/m2.out`、`logs/m3.out`）

| 操作 | アダプターの初期設定（mode=default） | `permissions.ask:["Bash"]` を注入したとき |
|---|---|---|
| Read（フォルダ内） | 許可を求めずに実行 | 同左 |
| Read `/etc/hosts`（フォルダ外） | 許可を求める（kind=read、path あり）→ 拒否。読まれなかった | 同左 |
| Write／Edit（フォルダ内） | 許可を求める（kind=edit、`rawInput.file_path`、`locations`）→ 許可して作成 | 同左 |
| Write（フォルダ外） | 許可を求める → 拒否。ファイルは作られなかった | 同左 |
| Bash `ls`、`whoami` | **許可を求めずに実行**（Claude Code が読み取り専用と判定） | 許可を求める → 許可リストで判定。`whoami` は拒否 |
| Bash `cat /etc/hosts`、`ls /Users`、`touch …`、`ls && touch …` | 許可を求める（`rawInput.command` に全文、`locations` に対象パス）→ 拒否。実行されなかった | 同左 |
| WebFetch／WebSearch | 許可を求める（どちらも kind=fetch、`name` で区別できる）→ 拒否 | 同左 |

**必須の設定**（これがないと約束が崩れる）:

1. **モードを `default` にする**。アダプターの初期値は `auto`（`session-mode.js`。auto モードが使えるなら auto になる）。auto のままだと、`touch` が**許可を求めずに実行された**（`logs/m9`）。`acceptEdits` では、編集が許可を求めずに通った（`logs/m8`）。
2. `_meta.claudeCode.options.settings.permissions.ask = ["Bash"]` を入れる。読み取り専用と判定されたコマンドも、すべて許可を求めるようになる。
3. `_meta.claudeCode.options.settingSources = []` を入れる。ユーザーの `~/.claude/settings.json` の allow ルールで、許可を求めずに通ってしまうのを防ぐ（session/new が通ることは確認。allow ルールを入れて素通りすることは**未検証**）。

あわせて、`allow_always`（`allow-with-updates`）は選ばない。選ぶと Claude Code の設定ファイルにルールが永続化される。

- `fs/*` と `terminal` は、Claude アダプターは使わない。Claude Code が自分で読み書きし、自分で実行する。したがって、フォルダの境界は「許可要求への応答」で守ることになる。
- ルートの許可リストは文字列の前方一致では足りない。プロトタイプの `ls` 許可では `ls /Users` も通ってしまった。`locations` を併用し、引数のパスがフォルダ外なら拒否する判定が必要。

### Codex（codex-acp、ソースから・未実行）

- モードは read-only／workspace-write／agent（Auto review）／full-access。初期値は **agent（承認者が LLM で、「危険そうなものだけ聞く」）**。SockDock 側で明示的に変える必要がある（`src/AgentMode.ts`）。
- 許可を求めるのは、コマンドについてはサンドボックスの外に出るとき（approval policy が `on-request` の場合）と、ファイル変更と、権限の追加要求に限られる（`permissions/CodexApprovalHandler.ts`）。サンドボックスの中のコマンドは、**1 つずつ許可を求めずに実行**する。コマンド許可リストは強制できない。approval policy を `untrusted` にすれば多くのコマンドで許可を求めるはずだが、`CODEX_CONFIG` で効くかどうかは**未検証**。
- Web 検索は、Codex 側でホストされるツールで、許可要求にならない。`web_search = "disabled"`（型 `WebSearchMode`）の設定で止める必要がある（**未検証**）。コマンドのネットワークは、サンドボックスの `networkAccess:false` で塞がれる。
- `fs/*` と `terminal` はクライアントに委ねない（本番コードから呼ばれていない）。

### Gemini（`--acp`、ソースから・未実行）

- 既定のポリシー（`core/src/policy/policies/read-only.toml`）では、`read_file`、`glob`、`grep_search`、**`google_web_search`** が許可を求めずに実行される。`write_file`、`replace`、`run_shell_command`、`web_fetch` は許可を求める。
- 許可要求に入るのは `kind`、`title`、`locations`、編集の diff。コマンドは主に `title` に入り、`rawInput` は無い。仕様は「title だけで自動承認するべきでない」としているので、注意が必要。
- クライアントが `fs` 機能を広告すると、ワークスペース内の読み書きは `fs/read_text_file`／`fs/write_text_file` を経由する（`acpFileSystemService.ts`）。SockDock の側でフォルダ内に限れる。シェルは自前で実行する（`terminal` は使わない）。
- ユーザーの `~/.gemini` のポリシーや設定で、許可を求めずに通ることがある。

## 4. 停止（session/cancel）

**実機で確認**（`logs/m6*`）:

- モデルの応答をストリーミングしている途中で cancel を送ると、**12ms で `stopReason: cancelled`** が返った。API への接続も切れた。
- 許可済みのコマンド（`perl -e 'sleep 30'`）の実行中に送ると、**15ms で `cancelled`** が返った。そのプロセスも残っていなかった。

仕様では、cancel を送ったら、保留中の許可要求に `cancelled` で応答する必要がある（<https://agentclientprotocol.com/protocol/v1/prompt-turn#cancellation>）。

- Codex: `turn/interrupt` を呼び、保留中の許可要求を中止する（ソース）。
- Gemini: `AbortController.abort()`（ソース）。

参考: Claude Code は、単独の `sleep N` をツールの段階で拒否する。

## 5. 指示・システムプロンプト・モデル

- **システムプロンプト**: 安定版仕様に `session/new.systemPrompt {mode:"append"|"override", content:[text]}` がある（<https://agentclientprotocol.com/protocol/v1/session-setup#system-prompt>）。Claude アダプターで `append` を送り、モデルへのリクエストの system に入ったことを確認した（`systemHasSockDockMarker=true`）。`load`／`resume` のときにも同じものを再送する。
  - Codex: `systemPrompt` を広告している（append。developer instructions になる）
  - Gemini: 広告していない。最初のメッセージに入れるか、`GEMINI.md`／`GEMINI_SYSTEM_MD` を使う（**未検証**）
  - Claude では `_meta.systemPrompt` も使える
- **モデル**: `session/new` の応答の `configOptions` に `category:"model"` の選択肢がある。`session/set_config_option` で `haiku` を指定し、リクエストの model が `claude-haiku-*` になったことを確認した。エフォートは `category:"thought_level"`。
  - Codex: model と reasoning effort の config option がある（README）
  - Gemini: `unstable_setSessionModel`（ソース）
- **モード**: `session/set_mode`（Claude は default／acceptEdits／plan／auto／bypassPermissions）。
- Claude 専用の機能として、`_meta.claudeCode.options` が Claude Agent SDK の Options をほぼそのまま通す（`tools`、`allowedTools`、`disallowedTools`、`settings`、`settingSources` など）。今の `claude -p` に近い制御もできる。

## 6. 使用量・コスト

- **Claude（実機で確認）**: `session/prompt` の応答の `usage` に、`inputTokens`／`outputTokens`／`cachedReadTokens`／`cachedWriteTokens`／`totalTokens` が入る。`usage_update` の通知では `used`／`size`（コンテキスト）と `cost {amount, currency:"USD"}` が届く。今の `total_cost_usd` の代わりに使える。
- Codex: `PromptResponse.usage` と `usage_update`（ソース）。
- Gemini: `usage_update` と、応答の usage（モデルごと）がある（ソース）。コスト（金額）は**未確認**。

## 7. エージェント比較

| | Claude（claude-agent-acp） | Codex（codex-acp） | Gemini（`gemini --acp`） |
|---|---|---|---|
| loadSession／resume | ✓／✓（実機） | ✓／✓（initialize） | ✓／✗（initialize） |
| 許可要求 | 編集・フォルダ外の読み取り・Web・コマンドで来る。`ask:["Bash"]` で全コマンド（実機） | ファイル変更・サンドボックス外への昇格だけ。コマンド単位では来ない（ソース） | 書き込み・シェル・web_fetch で来る。読み取りと Web 検索は来ない（ソース） |
| 必須の注入 | mode=default、ask Bash、settingSources=[] | mode を read-only か workspace-write に。Web 検索は設定で無効化 | 除外するツールの設定（**未検証**） |
| cancel | 約 15ms で停止（実機） | turn/interrupt（ソース） | abort（ソース） |
| systemPrompt | ✓ append／override（実機） | ✓ append（initialize） | ✗（最初のメッセージで代替） |
| モデル選択 | config option（実機） | config option（README） | `unstable_setSessionModel` |
| 使用量 | usage と USD コスト（実機） | usage（ソース） | usage（ソース） |
| 認証 | Claude CLI のログイン（または API キー） | ChatGPT ログインか `OPENAI_API_KEY`（`authMethods` で確認） | Google ログインか `GEMINI_API_KEY`（無い場合 `session/new` がエラーになることを確認） |
| インストール | Node 22 以上と npm パッケージ（約 395MB。Claude CLI のネイティブバイナリを同梱）。Node 不要の単体実行ファイルが GitHub Releases にある（README） | Node と npm パッケージ（約 398MB。`@openai/codex` を同梱）。bun による単体ビルドもある（公開状況は**未検証**） | Node と `@google/gemini-cli`（約 100MB） |

## 8. ルート UI の権限欄で約束できること

| UI の項目 | Claude | Codex | Gemini |
|---|---|---|---|
| Web 検索・取得 | ✓ 許可要求で拒否できる | △ 設定で無効化する（許可要求は来ない）。コマンドのネットワークはサンドボックスで遮断 | △ web_fetch は拒否できる。web_search は設定で除外が必要 |
| フォルダ内の編集 | ✓（フォルダ外は拒否） | ✓ OS サンドボックスで強制（workspace-write）。オフなら read-only + 許可要求 | ✓ 許可要求と `fs/*` の両方で強制できる |
| コマンド許可リスト | ✓（ask Bash の注入と、パスを見る判定が前提） | ✗ 「コマンドはサンドボックスの中で自由に実行」と表示するしかない | ○ 許可要求は来るが、コマンドは title にしか入らない |
| それ以外は拒否 | ✓（`allow_always` を使わない／ユーザー設定を読まない、が前提） | △ サンドボックス次第 | △ ユーザーの `~/.gemini` のポリシーに左右される |

UI としては、エージェントを選ぶと権限欄の文言が変わる形にするとよい。例として Codex を選んだら、許可リストの欄を隠して「サンドボックス内で実行」と表示する。保証できない項目は出さない。

## 9. パッケージングと配布

- ユーザーには **Node.js（Claude アダプターは 22 以上）** と、エージェントごとの npm パッケージが必要になる。Claude アダプターだけは、Node 不要の単体バイナリ（Claude CLI 同梱）を選べる。
- アダプターはどれも数百 MB あり、更新も速い（claude-agent-acp は 0.106、プレビューは main への push ごと）。**SockDock に同梱するのは不向き**。
- 推奨: 今の `claude_command` と同じように、「エージェントのコマンド」を設定で指定させる。既定値のプリセットは `npx -y @agentclientprotocol/claude-agent-acp`、`npx -y @agentclientprotocol/codex-acp`、`gemini --acp`。Homebrew の formula では、`node` を推奨依存（`depends_on` ではなく caveats）にとどめる。
- GUI から起動するときは、ログインシェルの PATH にならない（mise／nvm を使っている人は特に）。実行ファイルを絶対パスで解決する処理は、今の `resolveCommand` と同じものが要る。
- 認証はそれぞれの CLI で済ませてもらう。未ログインのときは `-32000 Authentication required`（Claude）や `session/new` のエラーで分かる。その場合はスレッドに「ターミナルで `claude`／`codex`／`gemini` にログインしてください」と返す。

## リスク

- **アダプターの初期値が緩い**: Claude は `auto`、Codex は Auto review。SockDock がモードを明示し忘れると、ユーザーへの約束が破れる。
- **権限要求はエージェントの裁量**: 同じ UI でも、守れる範囲がエージェントごとに違う。アダプターの更新で挙動が変わることもある。統合テスト（今回の `mockapi` 方式）を CI に置くとよい。
- **ユーザー設定が混ざる**: `~/.claude`、`~/.codex`、`~/.gemini` の allow ルールやフックが効いてしまう。分離方法はエージェントごとに違い、Claude 以外は**未検証**。
- **`_meta.claudeCode.*` は Claude 専用の拡張**で、仕様上の互換は保証されない。
- **仕様の更新が速い**: ACP v2 のドラフトがあり、Claude アダプターはすでに v1 と v2 の両方に応じる。Go SDK は追いついていない。
- **実モデルでは未検証**: ログインできなかったため。拒否されたあとのモデルの振る舞いや、実際のコスト表示は、ログイン済みの環境で 1 回確認する必要がある。
- **Codex と Gemini は未実行**（認証情報なし）。許可要求の中身、cancel、load は、ソースを読んだだけの判断。
