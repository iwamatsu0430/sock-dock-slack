# SockDock for Slack

**Slack アプリを手元で動かす: Socket Mode の接続は 1 つ、ハンドラはいくつでも。**

[English](README.md)

SockDock（コマンド名 `sockdock`）は、Mac で動く小さなゲートウェイです。Slack のワークスペースに届いた投稿を [Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode/) で受け取り（インターネットから届く口を開ける必要はありません）、設定したハンドラに渡します。

- **command 型**: 任意のプログラム。投稿を JSON で標準入力に受け取り、標準出力がスレッドへの返信になります。
- **claude 型**: [Claude Code](https://code.claude.com/) をヘッドレスで動かします。Slack のスレッド 1 つが 1 つの会話になり、返信のたびに続きから再開します。使えるツールは許可したものだけです。

電源タップのようなものです。Slack アプリの差込口は 1 つですが、SockDock を挟めばいくつでも差し込めます。トップレベルの投稿がルート（チャンネル＋正規表現）に一致するとスレッドが始まり、そのスレッドへの返信はすべて同じハンドラに渡ります。

CLI と、必要ならメニューバーのアプリで操作します。

> SockDock は Slack Technologies, LLC が作成・提携・サポートするものではありません。

**状況:** 開発の初期段階です。主な対象は macOS 13 以降です（CLI は Linux でも動きます）。

## はじめかた

### 1. インストール

[Homebrew](https://brew.sh/) で入れます。

```bash
brew install --cask iwamatsu0430/tap/sockdock   # メニューバーのアプリ
brew install --formula iwamatsu0430/tap/sockdock # CLI（macOS、Linux）
```

アプリは Apple の公証を受けていないため、初めて開くと macOS が「“SockDock” は開いていません」と表示して止めます。

1. **完了** を押します（「ゴミ箱に入れる」は押さない）。
2. **システム設定 → プライバシーとセキュリティ** を開き、下のほうにある **このまま開く** を押します。
3. 確認が出たら **開く** を押します。次からはそのまま開けます。

許可されるのは SockDock だけで、Mac の保護はそのまま働きます。Homebrew は、ダウンロードしたファイルが tap に記録されたハッシュと一致することを確かめてから入れます。Apple の説明: [Mac でアプリを安全に開く](https://support.apple.com/ja-jp/102445)

ソースからビルドすることもできます。ツールは [mise](https://mise.jdx.dev/) で管理しています。

```bash
git clone https://github.com/iwamatsu0430/sock-dock-slack.git
cd sock-dock-slack
mise install          # Go、Node、wails3
mise run build        # -> bin/sockdock
mise run app          # 任意: -> dist/SockDock.app（メニューバーのアプリ。CLI も同梱）
```

ビルドした場合は `bin/sockdock` を `PATH` の通った場所に置いてください（以下の例は `sockdock` が `PATH` にある前提です）。

### 2. Slack アプリを作る

```bash
sockdock manifest
```

1. <https://api.slack.com/apps?new_app=1> を開き、**From a manifest** を選んでマニフェストを貼り付けます。
2. **Basic Information → App-Level Tokens** で、`connections:write` のトークンを発行します（`xapp-…`）。
3. **Install App** でワークスペースにインストールし、**Bot User OAuth Token** をコピーします（`xoxb-…`）。
4. 振り分けに使うチャンネルに Bot を招待します: `/invite @SockDock`

必要なら: 返信に SockDock のアイコンを出すには、[`assets/slack-icon.png`](assets/slack-icon.png) をアプリのアイコンとして上げます（**Basic Information → Display Information**）。マニフェストではアイコンを決められません。

### 3. トークンを保存する

```bash
sockdock auth set     # 2 つのトークンを入力し、macOS のキーチェーンに保存する
sockdock auth status
```

環境変数 `SLACK_APP_TOKEN`・`SLACK_BOT_TOKEN` で渡すか、`settings.secrets_file` に権限 600 の `.env` ファイルを指定することもできます。

### 4. ルートを設定する

```bash
sockdock init         # "echo" ルート入りの設定の雛形を書き出す
mkdir -p ~/sockdock-handlers
cp examples/echo.sh ~/sockdock-handlers/   # そのルートが動かすハンドラ
```

設定ファイルを開き、`C0123456789` を使うチャンネルの ID に書き換えてください。

設定ファイルは `~/Library/Application Support/sockdock/config.yaml`（Linux は `$XDG_CONFIG_HOME/sockdock/config.yaml`）です。最小のルートは次のとおりです。

```yaml
routes:
  - name: echo
    channel: C0123456789            # チャンネル ID（チャンネル詳細の「概要」の一番下）
    starts_with: echo               # 「echo 〜」で始まる投稿
    handler:
      type: command
      cwd: ~/sockdock-handlers
      command: ["./echo.sh"]
```

反応する投稿の条件は、次の 4 つのうち 1 つを書きます。同じチャンネルでは上に書いたルールから順に見て、最初に合ったルールが動きます（オフのルールは飛ばします）。

| キー | 動く投稿 | `groups` |
|---|---|---|
| `starts_with: echo` | キーワードで始まる投稿 | `text`: キーワードの後ろ |
| `contains: 相談` | キーワードを含む投稿 | `text`: 投稿全体 |
| `match: '^deploy (?P<env>\w+)$'` | 正規表現（RE2）に一致する投稿 | 名前付きグループ |
| `all_posts: true` | すべての投稿 | `text`: 投稿全体 |

キーワードは、大文字と小文字、全角と半角、空白の数、先頭の Slack アプリへのメンションを区別しません。英数字のキーワードは単語の途中では一致しません（`entry` は `entryx` に一致しません）。正規表現は大文字と小文字を区別します。

確認します。

```bash
sockdock check
sockdock match C0123456789 "echo hello"
```

### 5. 動かす

```bash
sockdock run                # フォアグラウンドで動かす（Ctrl-C で止める）
sockdock service install    # または（メニューバーのアプリを使わない場合）: ログイン時に起動し、落ちたら起動し直す（launchd / systemd）
sockdock status
```

チャンネルに `echo hello` と投稿すると、SockDock が 👀 を付け、ハンドラを動かしてスレッドに返信します。

### メニューバーのアプリを使う場合

アプリを使えば、上の手順 3〜5 も YAML の編集も要りません。アプリケーションフォルダの SockDock を開きます（ソースからビルドした場合は `open dist/SockDock.app` または `mise run gui`）。

1. 初回はセットアップが出ます。「api.slack.com を開く」で、マニフェストが入った状態の作成ページが開きます。アプリをインストールし、`connections:write` のスコープで App-Level Token を発行したら、2 つのトークンを貼って「Slack に接続」を押します。トークンは Slack で確かめてから、2 つとも使えるときだけキーチェーンに保存します。すでにある Slack アプリを使うなら、セットアップの「すでにある Slack アプリを使う」を開いてください。必要な設定（Socket Mode、イベント `message.channels`・`message.groups`、Bot Token のスコープ）と、App Manifest に足す部分のコピーがあります。Socket Mode をオンにすると、そのアプリのイベントは SockDock にだけ届くようになるので気をつけてください。必須のスコープが足りなければ、「Slack に接続」で足りないものの名前が出ます。
2. 「ルール」→「ルールを追加」で、チャンネルを選び（Slack アプリを追加したチャンネルが一覧に出ます。`channels:read`・`groups:read` のスコープがなければ、チャンネル ID かリンクを入力します）、反応する投稿（〜で始まる・〜を含む・正規表現・すべて）を選び、実行するもの（作業フォルダで動く AI エージェント〈Claude Code〉と、その指示・許可する操作／またはプログラム）を選びます。一覧はチャンネルごとにまとまり、各ルールの最後の処理が出ます。同じチャンネルでは上のルールが優先され、⠿ をドラッグして並べ替えます。ルールを開くと、最近の処理、スレッドへのリンク、「停止」があります。
3. チャンネルに「<キーワード> …」のように投稿します。

ゲートウェイはアプリが開いている間アプリの中で動くので、別にサービスを入れる必要はありません。メニューバーのアイコンを押すと、接続の状態・処理中・最近の処理が出ます。ウィンドウは 1 つで、上の［ルール｜設定］で切り替えます。「設定」（⌘,）には、Slack の接続、ログイン時に起動、通知、言語（Slack に書く定型文の言語も兼ねます）、MCP、「初期状態に戻す」があります。「初期状態に戻す」は、トークン・ルール・処理の記録・アプリの設定を消してセットアップからやり直します（設定ファイルはゴミ箱に移します。ログは残します）。フォームにないことは「設定ファイルを開く」で YAML をエディタで開いて書きます。問題のあるルール（作業フォルダがない、上のルールに先に合うなど）は赤く表示され、そのルールだけが外れます。ほかのルールは動き続けます。

`sockdock service install` のサービスがすでに動いている場合、アプリはその状態を表示し、自分ではゲートウェイを動かしません。

## ハンドラ

どのハンドラにも次の JSON を渡します（command 型は標準入力）。

```json
{
  "route": "echo",
  "turn": 1,
  "channel": "C0123456789",
  "thread_ts": "1762920000.000100",
  "permalink": "https://example.slack.com/archives/C0123456789/p1762920000000100",
  "groups": { "text": "hello" },
  "messages": [{ "ts": "1762920000.000100", "user": "U0123", "text": "echo hello" }]
}
```

`groups` は最初の投稿から取り出した値（上の表）で、スレッドの 2 ターン目以降も同じものを渡します。

### command 型

```yaml
handler:
  type: command
  cwd: ~/my-handlers
  command: ["uv", "run", "python", "bot.py"]   # シェルは通さない
  env_file: .env                                # 任意。cwd からの相対パス
  timeout_sec: 300
```

- 標準出力をスレッドに投稿します（空なら何も投稿しません）。
- 環境変数: `SLACK_CHANNEL`、`SLACK_THREAD_TS`、`SOCKDOCK_ROUTE`、`SOCKDOCK_TURN`、それに `env_file`・`env` の値。Slack のトークンは渡しません。返信は、ハンドラが標準出力に書いたものです。
- 終了コードが 0 以外、またはタイムアウトなら短いエラーを返します。標準エラーはログにだけ残します。

### claude 型

```yaml
handler:
  type: claude
  cwd: ~/projects/my-project
  prompt: prompts/slack.md                   # 毎ターン、システムプロンプトに追加する
  allowed_tools: ["Bash(make test *)", "WebSearch", "WebFetch"]
  model: opus
  timeout_sec: 600
```

- `claude -p --output-format json --permission-mode dontAsk` を `allowed_tools` 付きで動かします。承認が要るほかの操作は拒否されます。Read・Glob・Grep は使え、MCP のツールは `allow_mcp: true` にしない限り使えません。
- スレッドごとにセッションを保存し、次の返信で再開します。再開できなければ新しい会話として始め、その旨をスレッドに伝えます。
- Claude が処理している間に届いた投稿は、次のターンでまとめて渡します。
- Claude Code はあなた自身のログインで動きます。`--bare` を使わないので、プロジェクトの `.claude/settings.json`・`CLAUDE.md`・フックも効きます。ルートに何を許すかは、それも含めて考えてください。
- `settings.claude_command` には絶対パスを書いてください。SockDock から Claude Code を起動するとき、バージョン管理ツール（mise、asdf など）のシムは動かないことが多いためです。メニューバーのアプリはログインシェルの `PATH` をハンドラに渡し、`sockdock service install` は実行したときの `PATH` をサービスに記録します。

## 設定のリファレンス

```yaml
settings:
  max_concurrency: 3        # 同時に動かすハンドラの数（変更は再起動で反映）
  thread_ttl_days: 30       # スレッドとルートの結びつきを保つ日数
  ack_reaction: eyes
  claude_command: claude    # 文字列かリスト。ルートの `command` があればそちらを使う
  language: auto            # Slack に投稿する定型文の言語: auto、en、ja
  path: ["/opt/homebrew/bin"]
  secrets_file: secrets.env
  messages:                 # 定型文の上書き。{route} と {timeout_sec} が使える
    timeout: "{timeout_sec} 秒で打ち切りました。"
```

- ルートの `${VAR}` は、そのルートの `env_file`、環境変数の順に探します。
- 知らないキーはエラーにするので、書き間違いは `sockdock check` で見つかります。
- 設定を変えたら `sockdock reload`（または `SIGHUP`、アプリでの保存）で読み直します。

## CLI

| コマンド | |
|---|---|
| `sockdock run` | ゲートウェイをフォアグラウンドで動かす |
| `sockdock init` / `check` / `match` | 設定の作成・検証・一致の確認 |
| `sockdock status` | 状態を表示する（動いていない・つながっていなければ終了コード 1） |
| `sockdock reload` | 設定を読み直す |
| `sockdock threads` / `turns` / `logs` | 最近のスレッド、処理、ログ |
| `sockdock pause <route>` / `resume <route>` | ルートの一時停止・再開 |
| `sockdock auth set` / `status` / `delete` | キーチェーンのトークンの管理 |
| `sockdock service install` / `uninstall` / `status` | メニューバーのアプリを使わない場合のバックグラウンドのサービス（launchd / systemd） |
| `sockdock manifest` | Slack アプリのマニフェストを出力する |
| `sockdock mcp` | MCP サーバーとして動く（下の「MCP」） |

データと制御用のソケットは `~/Library/Application Support/sockdock/`、ログは `~/Library/Logs/sockdock/` にあります。

## MCP

SockDock が動いている間（アプリでも `sockdock run` でも）、Claude Code・Codex などの MCP クライアントから操作できます。たとえば「#ask の投稿で ~/app の Claude Code が動くルールを作って」と頼めます。

登録のしかたは、アプリの「設定」の「MCP」でクライアントを選んでコピーするのがいちばん簡単です。手で登録するなら次のとおりです（`<sockdock>` は CLI の絶対パス。アプリなら `SockDock.app/Contents/MacOS/sockdock`）。

| クライアント | 登録のしかた |
|---|---|
| Claude Code | `claude mcp add -s user sockdock -- <sockdock> mcp` |
| Codex | `codex mcp add sockdock -- <sockdock> mcp` |
| Gemini CLI | `gemini mcp add -s user sockdock <sockdock> mcp` |
| VS Code | `code --add-mcp '{"name":"sockdock","command":"<sockdock>","args":["mcp"]}'` |
| Claude Desktop | アプリの「設定」→「MCP」で Claude Desktop を選び、［Claude Desktop に追加］を押す（拡張機能 `SockDock.mcpb` を作って開きます） |
| Cursor / その他 | 設定ファイルの `mcpServers` に `"sockdock": {"command": "<sockdock>", "args": ["mcp"]}` を足す |

ツールは、状態・ルール・最近の処理・チャンネルを見るもの、どのルールが動くかを試すもの、ルールの追加・変更・削除・並べ替え・オン/オフ、処理の停止です。

- MCP からルールを変えると、Mac に通知が出ます（通知の設定がオフでも出ます）。
- トークンと投稿の本文は MCP に渡しません。
- SockDock が動かした処理の中（Slack から動いたエージェント）では、`sockdock mcp` は起動しません。

くわしくは [docs/mcp.md](docs/mcp.md) を見てください。

## 開発

```bash
mise tasks            # タスクの一覧
mise run ci           # vet、テスト、フロントエンドの型チェック
mise run gateway      # ビルドしてゲートウェイを動かす
mise run gui          # SockDock.app をビルドして開く
mise run gui-preview  # GUI を http://localhost:8080 でブラウザに表示する（トレイなし）
```

開発中に本物の設定を触らないよう、`SOCKDOCK_DATA_DIR` と `SOCKDOCK_CONFIG` を設定できます（`mise.toml` の `[env]` を参照）。

構成: `cmd/sockdock`（CLI）、`internal/`（ゲートウェイ、ハンドラ、制御 API など）、`gui/`（Wails v3 ＋ Svelte）、`examples/`（ハンドラの例）、`scripts/`（`.app` の組み立て。mise なしでも使える）、`docs/`（設計メモ）。

## ライセンス

[MIT](LICENSE)
