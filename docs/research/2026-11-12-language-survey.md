# テックスタックの調査: SockDock for Slack（2026-11-12、改訂）

SockDock for Slack（旧称 slack-socket）を **OSS ／配布するツールとして、Mac に常駐するプロセス** として作る前提で、言語とフレームワークを特定の言語に寄せずに比べた。将来 GUI（メニューバーの常駐アプリなど）を作る可能性も含める。

> 初版は「個人用スクリプト」を前提にしていて、Python を推していた。前提が変わったので書き直した。

## 結論

- **構成は「コア（ヘッドレスの常駐プロセス＋CLI）」と「GUI（任意。コアを操作する薄いクライアント）」に分ける。** コアはローカルの制御 API（Unix ドメインソケット）を持ち、CLI も GUI もそこを通す。こうしておけば、GUI の技術はあとから選べる。GUI が落ちてもゲートウェイは止まらない。
- **コアは Go を推奨する。** 理由は 4 つ。
  1. 1 つのバイナリで配れる。Homebrew の formula、`go install`、GitHub Releases のどれでも配りやすく、Linux のサーバーでも同じものが動く
  2. 常駐プロセスに向いている。メモリが少なく、子プロセスの管理（プロセスグループ、シグナル、タイムアウト）や「スレッドごとに直列、全体では並行」という処理が goroutine とチャネルで素直に書ける
  3. 同じ分野で実績がある。Slack と Claude Code をつなぐ OSS で最も使われている cc-connect（約 1.6 万スター）も Go で書かれている
  4. Slack SDK が公式でない弱みが小さい。Socket Mode の手順は小さく（URL を取得 → WebSocket → `envelope_id` を返す → 切断の予告が来たら張り直す）、slack-go/slack の `socketmode` を使っても、自分で書いても済む
- **GUI は、作るときに次のどちらかにする。** いまは作らない。
  - Mac のネイティブアプリ（Swift の `MenuBarExtra`）。コアを内蔵し、`SMAppService` でログイン項目として登録する。Mac での体験は最もよい
  - Wails v3（Go ＋ Web の UI。メニューバーへの常駐もできる）。言語が 1 つで済む。（訂正: 調査時はベータと判断したが、実際には v3.0.0 が安定版として出ており、2026-11-01 に v3.1.0 が出ている）
- **次点**
  - Rust ＋ Tauri v2: GUI を最初から主役にするなら最有力。コアと GUI を 1 つの言語・1 つのアプリで作れて、Tauri v2 は安定版。ただし実装の手間は最も大きい
  - TypeScript: Slack 公式の Bolt がそのまま使えるのが最大の利点。ただし実行環境が要る（Node／Bun、または 60〜100MB 程度の単体バイナリ化）。GUI を Electron にすると、常駐アプリとしては重い
  - C#（.NET 10）: Native AOT で 1 つのバイナリにでき、Avalonia でトレイ常駐の GUI も作れる。手元に開発環境があり、ほかのプロジェクト（madorisim）でも使っている。Slack SDK は SlackNet（コミュニティ製）
- **Python は外す。** 配布に実行環境が要り（`uv tool install` などで開発者には配れるが、一般向けには弱い）、GUI の選択肢も乏しい。個人用のスクリプトなら最良だが、今回の前提には合わない。

最終的な選択は、慣れ（Go と Rust は手元に開発環境がない）と、GUI をどのくらい重く考えるかで決まる。

## 評価の観点

| 記号 | 観点 |
|---|---|
| ① | 配布のしやすさ（単体バイナリ、Homebrew、署名・公証の要否） |
| ② | 常駐プロセスとしての質（メモリ、起動、子プロセスの管理、並行処理） |
| ③ | Slack SDK（公式か、Socket Mode の完成度） |
| ④ | GUI への道筋（メニューバー常駐、ログイン時の起動） |
| ⑤ | Mac 以外（Linux のサーバー）でも動かせるか |
| ⑥ | 実装と保守の手間、慣れ |

## 比較

| | ① 配布 | ② 常駐 | ③ Slack SDK | ④ GUI | ⑤ Linux | ⑥ 手間・慣れ |
|---|---|---|---|---|---|---|
| **Go** | ◎ 単体バイナリ（10〜20MB）。Homebrew の formula で配れば公証は不要 | ◎ | ○ slack-go/slack（コミュニティ製、広く使われている）。自作も容易 | ○ Wails v3（安定版 v3.1.0）か、Swift のアプリからコアを呼ぶ | ◎ | ○ 書きやすいが手元に環境なし |
| **Rust** | ◎ 単体バイナリ | ◎ | ○ slack-morphism（コミュニティ製、Socket Mode 対応、更新が続いている） | ◎ Tauri v2（安定版）で同じ言語のまま GUI とトレイ常駐 | ◎ | △ 実装に最も時間がかかる。手元に環境なし |
| **Swift** | △ GUI アプリは署名と公証が実質必須（Apple Developer Program、年額） | ◎ Mac の機能（スリープ通知、キーチェーン、通知）を直接使える | △ SDK なし。Socket Mode と Web API を自作 | ◎ `MenuBarExtra`、`SMAppService` | ✕ Mac 専用 | ○ |
| **TypeScript** | △ 実行環境が要る。単体バイナリ化は大きい。npm での配布は開発者には自然 | ○ | ◎ 公式 Bolt。新機能も最初に出る | △ Electron は重い。Tauri と組み合わせると言語が 2 つになる | ◎ | ○ 手元に Node なし |
| **C#（.NET 10）** | ○ Native AOT で単体バイナリ | ○ | ○ SlackNet（コミュニティ製、Socket Mode 対応） | ○ Avalonia でトレイ常駐 | ◎ | ◎ 手元に環境があり、使い慣れている |
| **Python** | ✕ 実行環境が要る | ○ | ◎ 公式 Bolt | ✕ rumps 程度 | ◎ | ◎ |

### 補足

- **署名と公証**: 配布物が GUI アプリ（.app）なら、Gatekeeper の警告を避けるために Developer ID での署名と公証が実質的に必要になる。CLI のバイナリを Homebrew の formula で配る場合は不要。コアを CLI として先に出し、GUI をあとから出す順番だと、この負担を後ろに回せる。
- **ログイン時の起動**: CLI 版は launchd の LaunchAgent（plist を `~/Library/LaunchAgents` に置く。`brew services` でも可）。GUI 版は `SMAppService.agent` でアプリに同梱した LaunchAgent を登録する（macOS 13 以降。ユーザーはシステム設定の「ログイン項目と機能拡張」で切り替えられる）。
- **Socket Mode の要点**（自作する場合）: `apps.connections.open` に App-Level Token を渡して WebSocket の URL を得る。接続すると `hello` が届き、イベントごとに `envelope_id` を返す。`disconnect` には `warning`（約 10 秒後に切れる）・`refresh_requested`（すぐ張り直す）・`link_disabled`（Socket Mode が無効にされた）がある。接続は数時間ごとに更新される。1 アプリにつき最大 10 本まで接続でき、イベントはどれか 1 本に届く。
- **スリープ復帰**: どの言語でも、SDK の ping による切断検知に加えて、自前の見張り（壁時計の飛びを検知して張り直す、長くつながらなければ終了して launchd に起動し直させる）を入れる。Swift なら `NSWorkspace` のスリープ／復帰の通知を直接受けられる。

## Claude の実行方式（言語によらない）

| 方式 | 評価 |
|---|---|
| **`claude -p` を子プロセスで起動**（採用） | ローカルの Claude Code のログイン（サブスクリプション）で動く。`--output-format json` で `session_id` と最終応答を受け、`--resume` で続ける。`--allowedTools`・`--tools`・`--permission-mode dontAsk` でツールを絞れる。起動するコマンドは設定で差し替えられる。言語を選ばない |
| Claude Agent SDK | Python と TypeScript だけ。公式ドキュメントに「第三者の開発者が claude.ai のログインを自分の製品で使うことは、承認がない限り認めない」とあり、OSS として配るなら利用者に API キーを求めることになる |

OSS として配る場合、「claude 型」は Claude Code 専用にせず、「会話を続けられるエージェント CLI」を差し替えられる作りにしておく（Codex CLI など）。最初の実装は Claude Code だけでよい。

## 出典

- [Slack: Using Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode/)
- [Bolt for Python: Socket Mode](https://docs.slack.dev/tools/bolt-python/concepts/socket-mode)、[Python Slack SDK の aiohttp 版 SocketModeClient](https://github.com/slackapi/python-slack-sdk/blob/main/slack_sdk/socket_mode/aiohttp/__init__.py)
- [Node Slack SDK: SocketModeOptions](https://docs.slack.dev/tools/node-slack-sdk/reference/socket-mode/interfaces/SocketModeOptions)
- [slack-go/slack の socketmode の例（pkg.go.dev）](https://pkg.go.dev/github.com/slack-go/slack@v0.24.0/examples/socketmode)
- [slack-morphism（docs.rs）](https://docs.rs/crate/slack-morphism/2.24.1)
- [Wails v3 のリリース一覧](https://github.com/wailsapp/wails/releases)、[Wails v3 ベータの告知](https://docs-preview.wails.io/blog/wails-v3-beta/)
- [Apple: Updating helper executables from earlier versions of macOS（SMAppService）](https://developer.apple.com/documentation/servicemanagement/updating-helper-executables-from-earlier-versions-of-macos.md)
- [Node.js: node:sqlite](https://nodejs.org/download/release/latest-lithium/docs/api/sqlite.html)
- [Claude Code: Run Claude Code programmatically](https://code.claude.com/docs/en/headless.md)、[CLI reference](https://code.claude.com/docs/en/cli-reference.md)、[Agent SDK overview](https://code.claude.com/docs/en/agent-sdk/overview.md)
- [cc-connect](https://github.com/chenhg5/cc-connect)
