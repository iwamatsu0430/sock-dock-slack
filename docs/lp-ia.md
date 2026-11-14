# SockDock for Slack — LP の情報設計

GitHub Pages に置く紹介ページ（LP）に何を載せるかを決める文書です。LP を変えるときは、先にこの文書を直します。アプリの画面の情報設計は [`ia.md`](ia.md)、用語は ia.md §9 に合わせます。
モック: [`docs/mock/lp.html`](mock/lp.html)（英語と日本語を 1 ファイルで切り替えて確かめる）
サイト: [`site/`](../site/)（英語 `site/index.html`、日本語 `site/ja/index.html`。`main` に push すると `.github/workflows/pages.yml` が GitHub Pages に出す）

LP を変えるときは、この文書 → モック → サイトの順に直します。

## 1. 拠りどころにする一般論

| 一般論 | 要点 | この LP での使い方 |
|---|---|---|
| 10 秒で価値を伝える（NN/g） | 訪問者の多くは 10〜20 秒で去る。30 秒を越えた人は 2 分以上とどまる。読まれるのは文字の 4 分の 1 ほど | 最初の画面だけで「何か・自分向けか・どう始めるか」がわかるようにする。本文は見出しだけ読んでも通じる形にする |
| 注意はページの上に集まる（NN/g） | 閲覧時間の 57% が最初の画面、74% が 2 画面目まで、81% が 3 画面目まで | ページ全体を 3 画面ほどに収める。下に行くほど「決めたあとの人」向けにする |
| ニオイ（情報の匂い）（NN/g） | 来る前に見た言葉と、来て最初に見る言葉が合っていないと去る | 入口（GitHub の README、紹介の投稿）と同じ言葉で書き出す。名前・アイコン・見出しを README とそろえる |
| 認知の段階（Schwartz） | 読み手が「問題・解き方・この製品」のどこまで知っているかで、書き出しを変える | 主な訪問者は「Slack から AI エージェントを動かしたい」と解き方まで知っている人。問題の説明から入らず、何が起きるかから入る |
| 開発者向け LP 100 件の調査（Evil Martians） | 売り込みの言葉を使わない。主張より具体（数字・動くもの）。CTA は具体的な動詞で 2 つまで（主・副）。新しい製品の機能は「問題」の形で書く | 主な絵は Slack のスレッドそのもの。機能の一覧は作らず、利用者が決めること（どこまで許すか）を書く |
| Front doors（Dan Brown） | 利用者の多くは、トップ以外の入口から来る | LP に来るのは README・投稿のリンクから。LP は README の要約で、詳しいことは README に任せる（同じことを 2 か所で書かない） |

出典: [NN/g, How Long Do Users Stay on Web Pages?](https://www.nngroup.com/articles/how-long-do-users-stay-on-web-pages/) / [NN/g, Scrolling and Attention](https://www.nngroup.com/articles/scrolling-and-attention/) / [Evil Martians, We studied 100 dev tool landing pages](https://evilmartians.com/chronicles/we-studied-100-devtool-landing-pages-here-is-what-actually-works-in-2025) / [Dan Brown, Eight Principles of Information Architecture](https://principles.design/examples/eight-principles-of-information-architecture) / Eugene Schwartz『Breakthrough Advertising』（1966）

**方針: README の縮小版を作らない。** LP の仕事は「自分向けだと 10 秒でわかり、始め方へ進ませる」ことだけ。設定・CLI・ハンドラの仕様は README に置き、LP からリンクする。

## 2. 訪問者の文脈

| 誰が | どこから来るか | 知っていること | 知りたいこと |
|---|---|---|---|
| Claude Code やスクリプトを日常的に使う Mac の開発者。自分や家族のために、家の Mac で使う（主） | 紹介の投稿（X・Zenn・Hacker News など）、GitHub のリポジトリ、検索 | Slack のボット、Claude Code。Socket Mode は知らないことが多い | これは何か。自分の Slack とMac で使えるか。危なくないか。すぐ試せるか |
| その人の家族（副） | 家族の Slack のチャンネル | Slack | 何を書けば何が起きるのか |

SockDock は家で使うためのもの（ホームユース）です。会社の Slack に入れる使い方は想定しないので、LP も会社の管理者の疑問（スコープ、承認、監査）には答えません。LP からは「家で使うもの」だと伝わればよい。

いちばん伝わりやすいのは、利用者が毎日見ている **Slack の画面で起きること**（投稿 → 👀 → スレッドに返信）です。アプリの画面は、使う場面ではほとんど見ないので（ia.md §2）、LP の主役にしません。

主な訪問者は、公式の Claude in Slack や Cursor・Codex の Slack 連携を知っているか、試したことがあります。LP は他の製品の名前を出さずに、「それとどう違うのか」に答える必要があります（§3）。

## 3. ほかとの違い

### 似たもの（2026-11 時点）

| | どこで動くか | 何に触れるか | どの投稿で何が動くかを決めるのは | AI 以外のプログラム |
|---|---|---|---|---|
| 公式の Claude in Slack / Claude Tag | クラウドのセッション | GitHub に接続したリポジトリ | AI（コードの依頼かどうか、どのリポジトリかを会話から判断） | なし |
| Cursor・Codex・Devin の Slack 連携 | クラウド | GitHub のリポジトリ。PR を作る | AI（@メンションの内容から） | なし |
| オープンソースの Slack ↔ Claude Code の橋渡し（blade、claude-slacker、bender など） | 手元のマシン（Socket Mode） | 手元のファイル | 多くは、すべてのメッセージが 1 つのエージェントに届く。設定はコードや環境変数 | 多くはなし |
| Hubot・Bolt で書くボット | 自分のサーバーか手元 | 書いたコード次第 | 書いたコード | ある（自分で書く） |
| **SockDock** | **手元の Mac（Socket Mode）** | **ルールで選んだフォルダ** | **ルール（チャンネル × 反応する投稿 → フォルダ・許可・動かすもの）** | **ある（どんなプログラムでも）** |

出典: [Claude Code in Slack](https://code.claude.com/docs/en/slack) / [Cursor, Slack](https://docs.cursor.com/slack) / [Slack, The Rise of Software Engineering Agents in Slack](https://slack.com/blog/developers/coding-agents-in-slack) / [digimata/blade](https://github.com/digimata/blade) / [helmcode/bender](https://gh.r-mj.com/helmcode/bender)。オープンソースの橋渡しは、すべての実装を確かめたわけではない（「多くは」）。

### ユニークな点

**Slack の投稿を合図に、手元の Mac にあるいつもの Claude Code やプログラムを、ルールで決めたとおりに動かす。**

1 文に入っている 3 つは、どれか 1 つなら他にもあります。3 つがそろっているのは SockDock だけです。

| 点 | 中身 | なぜ利用者にうれしいか |
|---|---|---|
| **ルールで決める**（いちばんの違い） | 「どのチャンネルの、どんな投稿で、どのフォルダの、何を、どこまでの許可で動かすか」をルールが決める。AI はリポジトリも許可も選ばない | 投稿する前に、何が起きるかがわかる。ルールごとに許可を絞れる（読むだけのルール、テストまで走らせてよいルール）。1 つの Slack アプリに、エージェントとスクリプトを何本でも差せる（Socket Mode では 1 つのアプリの投稿が接続の 1 本にしか届かないので、ふつうは用途ごとに Slack アプリを作ることになる。その手間をここで解く） |
| **手元の Mac で動く** | エージェントは、ルールで選んだ作業フォルダで動く。クラウドの環境も、外から入る口も要らない | 手元のファイル・ツール・コミット前の変更・社内のサービス・GitHub 以外のリポジトリにそのまま届く。コードが Mac の外の作業環境に出ていかない |
| **いつもの Claude Code** | 公開されているままの Claude Code を、利用者自身のログインで動かす。`CLAUDE.md`・設定・フックもそのまま効く | 新しい AI サービスも API キーも要らない。手元で育てた設定がそのまま Slack からも効く |

### 「サブスクリプション」を売りにしない

「手持ちのサブスクリプションで動く」は事実ですが、LP の売り文句にはしません。

- Anthropic は、Pro・Max などのプランの利用上限を「個人の通常の使い方」を前提にしていると定めています。SockDock は、ユーザーを制限しなければ投稿できる人なら誰でも動かせるので、「サブスクリプションをチームで使える」と受け取られる書き方は避けます。
- Anthropic は 2026 年に、プランの認証をほかのツールで使うことを禁じました。SockDock は、手を加えていない Claude Code を利用者自身のログインで動かすだけなので、この禁止には当たりません（規約が許している使い方）。ただ「サブスクリプションで」を前に出すと、禁止されたツールと同じものに見えます。
- 利用者にとって大事なのは「お金がかからないこと」より「いつもの Claude Code がそのまま動くこと」です。だから「いつもの Claude Code（自分のログイン）」と書きます。
- 「Claude Code」の名前は「Claude Code を動かす」と文中で書くだけにします。ロゴや、公式・提携に見える書き方はしません（Anthropic の規約）。

出典: [Claude Code, Legal and compliance](https://code.claude.com/docs/en/legal-and-compliance)

## 4. 訪問者の問いの順番

上から順に読む人の問いに、上から順に答えます。

| 順 | 訪問者の問い | 答える場所 | 答え |
|---|---|---|---|
| 1 | これは何？ | 最初の画面: 見出し | 投稿の言葉ごとに、動かすものを決めておける。AI エージェントでも、ゲームでも |
| 2 | 誰が何を決めるの？ 誰が動かせるの？ | 最初の画面: ルールで決める 3 つ | チャンネル・反応する投稿・実行するものを、ルールで決める。チャンネルに投稿できる人は誰でも動かせる。実行できるユーザーを制限することもできる |
| 3 | 本当にそう動く？ | 最初の画面: スレッドの絵 | 投稿 → 👀 → 返信、スレッドで続きを頼める |
| 4 | 自分の環境で使える？ いくら？ | 最初の画面: CTA の下の 1 行 | macOS · 無料・MIT |
| 5 | 公式の Claude in Slack などと何が違う？ クラウドで動くの？ | 2 画面目: 自分の Mac で動く | 手元のファイルを使って、いつもの Claude Code が動く。サーバーは要らない |
| 6 | 始めるのに何がいる？ | 3 画面目: 始め方 | Slack アプリを作る → 2 つのトークンを貼る → ルールを作る |
| 7 | どこから手に入れる？ | 最初の画面 | `brew install` の 1 行（ページで 1 回だけ） |
| 8 | 開けない。警告が出たけど大丈夫？ | 最後: 初めて開くとき（最初の画面の注意からリンク） | 公証を受けていないので止められる。「完了」→「このまま開く」→「開く」で開ける。許可されるのは SockDock だけ |

## 5. 構成

```
最初の画面   名前とアイコン · 見出し（例 2 つ）
             ルールで決める 3 つ · CTA（主: brew のコマンド / 副: GitHub） · 条件の 1 行 ｜ スレッドの絵 2 つ（縦に並べる）
2 画面目     自分の Mac で動きます（3 つ）
3 画面目     始め方（3 手順と、それぞれの補足） · Slack での動き · README へのリンク
最後         初めて開くとき（警告への対処 3 手順 · 安全について · Apple のサポートへのリンク）
フッター     GitHub · README · MIT · Slack 公式ではない旨
```

- 見出しのすぐ下に「ルールでは、次の 3 つを決めます」を置き、スレッドの絵はその横（狭い画面では CTA の下）に置く。
- 1 ページだけ。メニューは置かない（項目が少なく、上から読めば足りる）。
- 英語（`/`）と日本語（`/ja/`）の 2 つ。README と同じ組み合わせ。
  - `/` を開いたとき、システムの言語が日本語なら `/ja/` へ移る。GitHub Pages は静的な配信なので、ブラウザのスクリプトで行う。`/ja/` を直接開いた人はそのまま（日本語のリンクを送られた人）。
  - 右上でも切り替えられる。選んだ言語はブラウザに覚えておき、次に `/` を開いたときはシステムの言語より優先する。
  - URL を言語ごとに分けたままにするのは、検索エンジンに両方の言語を載せ、言語を指定してリンクを送れるようにするため。
- 置き場所: リポジトリの `site/` を GitHub Actions で Pages に出す（リポジトリの Settings → Pages の Source を「GitHub Actions」にしておく）。`docs/` は日本語の設計メモなので混ぜない。URL は `https://iwamatsu0430.github.io/sock-dock-slack/`。

## 6. 各部分と文言

「伝えたいこと」は、その部分を読んだ訪問者に持ってほしい考えです。画面には文言だけを載せます。英語は日本語の訳ではなく、同じ構成で別に書きます。

### 最初の画面: 見出し

| 要素 | 訪問者の問い | 伝えたいこと | なぜ要るか |
|---|---|---|---|
| 名前とアイコン | どこに来た？ | README・投稿で見たものと同じ | ニオイを途切れさせない |
| 見出し（例 2 つ） | これは何？ | 投稿の言葉ごとに、動かすものを決めておける。AI エージェントにも、仕事以外のことにも使える | 同じ形の文を 2 つ重ねると、違う部分（投稿の言葉と、動かすもの）が「自分で決める部分」だと説明なしで伝わる。1 つ目で AI エージェントを最初から見せ、2 つ目で幅を見せる |

| | 日本語 | 英語 |
|---|---|---|
| 1 行目 | Slack に「ask」と投稿されたら、AI エージェントを動かす。 | Post “ask” in Slack, run your AI agent. |
| 2 行目 | Slack に「遊ぼう」と投稿されたら、ゲームを起動する。 | Post “play”, launch a game. |

- AI エージェントを Claude Code に絞らない。いま動かせるのは Claude Code だけなので、それは「3 つの手順で始める」の実行するものの説明で「いまは Claude Code」と一度だけ書く（ほかのエージェントは ACP で足す予定: ia.md §11）。
- 投稿の言葉（「ask」「遊ぼう」）と、動かすもの（AI エージェント・ゲーム）に色を付けて、自分で決める部分だと示す。
- 見出しに「Mac で」を入れない。ほかの環境も選べると誤解されるため。Mac で動くことは、すぐ下の 3 つ目の項目と条件の 1 行で伝える。
- 日本語の 2 行目は日本語の話し言葉にして、投稿の言葉を自由に決められることも伝える。

### 最初の画面: ルールで決める 3 つ

| 要素 | 訪問者の問い | 伝えたいこと | 日本語 | 英語 |
|---|---|---|---|---|
| 導入 | 誰が何を決めるの？ | 見出しの 2 つは、どちらもルール | ルールでは、次の 3 つを決めます。 | A rule sets three things. |
| 1 | 誰が動かせるの？ | チャンネルで決まる。そこに投稿できる人は誰でも動かせる（正直に言う）。実行できるユーザーを制限することもできる | **どのチャンネルの投稿に反応するか** / 例: `#個人開発`。このチャンネルに投稿できる人なら誰でも、このルールを動かせます。実行できるユーザーを制限することもできます。 | **Which channel** / e.g. `#side-project`. Anyone who can post there can trigger the rule, or restrict it to specific users. |
| 2 | 関係ない投稿も読まれるの？ | 当てはまらない投稿では何も起きない | **どんな投稿に反応するか** / 例: 「ask」で始まる投稿。当てはまらない投稿では、何も起きません。 | **Which posts** / e.g. posts that start with “ask”. Any other post does nothing. |
| 3 | AI に何でもさせるの？ どこで動くの？ | Mac のどのフォルダで、何を許して動かすかを決める | **Mac で何を実行するか** / 例: `~/app` フォルダで AI エージェント。ファイルの読み取りだけを許可します。 | **What runs on your Mac** / e.g. an AI agent in `~/app`, allowed only to read files. |

実行できるユーザーの制限は、一定のリテラシーがある層へのアピールなので、「制限」「ユーザー」のような開発の言葉で書く（ユーザー確認済み 2026-11-14）。

### 最初の画面: そのほか

| 要素 | 訪問者の問い | 伝えたいこと | 日本語 | 英語 |
|---|---|---|---|---|
| CTA（主） | どうすれば手に入る？ | 1 行で入る。開発者には、ボタンより動くコマンドのほうが具体的（Evil Martians） | `brew install --cask iwamatsu0430/tap/sockdock`（コピーのボタン付き。メニューバーのアプリ） | 同じ |
| 初回の注意 | 開けなかったら？ | 公証を受けていないので、初回は止められる。手順は下にある | アプリは Apple の公証を受けていないため、初めて開くときに確認が要ります。[開き方](#first-open) | The app is not notarized by Apple, so opening it the first time takes an extra step. [How to open it](#first-open) |
| CTA（副） | 中身を見てから決めたい | コードはすべて公開されている | GitHub で見る | View on GitHub |
| 条件の 1 行 | 自分の環境で使える？ いくら？ | 使える条件と費用 | macOS 13 以降 · 無料・オープンソース（MIT） | macOS 13+ · Free and open source (MIT) |
| スレッドの絵 | 本当にそう動く？ 家で使うもの？ | 投稿 → 👀 → 返信 → 続きを頼める。自分の開発にも、家族の遊びにも使う | （下の会話） | （下の会話） |

スレッドの絵は、見出しの 2 行に合わせて 2 つ置く。Slack の画面の写しではなく、Slack 風の簡単な絵にする。日本語のページでは会話も日本語にする。家で使うものだと、言葉で説明せずに場面で伝える。

1 つ目 `#個人開発`（英 `#side-project`）: 自分の開発を、外出先から頼む。続きもファイルの読み取りだけで答えられる質問にして、3 つ目の項目の例（読み取りだけを許可）と食い違わないようにする。

1. Aki: 「ask ログインのテストが落ちるのはなぜ？」 / `ask why does the login test fail?`
2. 投稿に 👀
3. SockDock: 「`test/login.spec.ts` は 302 を期待していますが、ハンドラは 303 を返すようになっています。」 / `test/login.spec.ts expects a 302, but the handler now returns 303.`
4. Aki（スレッドで）: 「どこを直せばいい？」 / `where should I fix it?`
5. SockDock: 「`test/login.spec.ts` の 12 行目で、302 を 303 に変えれば通ります。」 / `Change 302 to 303 on line 12 of test/login.spec.ts.`

2 つ目 `#家族`（英 `#family`）: 家族が書いて、家の Mac でゲームが起動する。

1. Hana: 「遊ぼう」 / `play`
2. 投稿に 👀
3. SockDock: 「ゲームを起動しました。」 / `Game launched.`

### 2 画面目: 自分の Mac で動きます

§3 の「手元の Mac で動く」「いつもの AI エージェント」を、他の製品の名前を出さずに書く。

| 要素 | 訪問者の問い | 伝えたいこと | 日本語 | 英語 |
|---|---|---|---|---|
| 見出し | 公式のものと何が違う？ | クラウドではなく、自分の Mac で動く | 自分の Mac で動きます | It runs on your own Mac |
| 1 | クラウドの環境に何を用意するの？ | 手元のものにそのまま届く | **手元のファイルをそのまま使う** / AI エージェントは Mac のフォルダで動くので、コミット前の変更も、Mac に入れたツールも使えます。 | **Your files and tools, as they are** / The AI agent works in a folder on your Mac, so it sees uncommitted changes and the tools you installed. |
| 2 | 別の AI サービスに登録するの？ | いつもの AI エージェントが、いつもの設定で動く | **いつもの AI エージェント** / 自分のログインで動き、`CLAUDE.md` などの指示や設定もそのまま効きます。API キーは要りません。 | **Your own AI agent** / It runs with your login, your instructions (like `CLAUDE.md`) and settings. No API key. |
| 3 | サーバーを立てるの？ Mac を公開するの？ | Mac から Slack へつなぐだけ | **サーバーは要りません** / Mac から Slack へつなぐので、ポートを開けたり、公開 URL を用意したりしなくて済みます。 | **No server to set up** / SockDock connects from your Mac to Slack. No open ports, no public URL. |

「サブスクリプション」の語は使わない（§3）。

### 始め方

使い方の詳細は、ここで補う。最初の画面で「何ができるか」をつかんだ人が、「実際にどう使うのか」を確かめる場所。何に使うか（使いみち）は書かない。

| 要素 | 訪問者の問い | 伝えたいこと | 日本語 | 英語 |
|---|---|---|---|---|
| 見出し | 始めるのに何がいる？ | 3 手順で終わる | 3 つの手順で始める | Set up in three steps |
| 1 | Slack 側に何を設定するの？ | 細かい設定は作成ページに入っている。使うチャンネルに追加するだけ | **Slack アプリを作成** / SockDock のボタンから、必要な権限を入れた作成ページが開きます。作ったアプリを、使うチャンネルに追加します。 | **Create a Slack app** / A button in SockDock opens Slack's create page with the permissions filled in. Then add the app to the channels you'll use. |
| 2 | トークンって何？ | 2 つの役目と、置き場所 | **トークンを 2 つ貼る** / 投稿を受け取る App-Level Token（`xapp-`）と、返信する Bot Token（`xoxb-`）です。どちらも Mac のキーチェーンに保存します。 | **Paste its two tokens** / An App-Level Token (`xapp-`) to receive posts and a Bot Token (`xoxb-`) to reply. Both are kept in your Mac's Keychain. |
| 3 | ルールはどこで作るの？ 何を実行できるの？ | メニューバーのアプリで作る。AI エージェントでも、自分で書いたプログラムでも | **ルールを作る** / メニューバーのアプリで、チャンネル・反応する投稿・実行するものを選びます。 | **Add a rule** / In the menu bar app, choose a channel, which posts, and what runs. |
| 3 の補足 | — | 実行するものは 2 種類 | ・**AI エージェント（いまは Claude Code）**: 選んだフォルダで動きます。ファイルの読み取りは常に、編集・Web 検索・コマンドは許可したときだけできます。<br>・**プログラム**: 自分で書いたスクリプトなど。標準出力がスレッドへの返信になります。 | • **AI agent (Claude Code for now)**: runs in the folder you pick. It can always read files; editing, web search and commands only if you allow them.<br>• **Program**: any script you write. What it prints becomes the reply. |

#### Slack での動き

| 要素 | 訪問者の問い | 伝えたいこと | 日本語 | 英語 |
|---|---|---|---|---|
| 見出し | 投稿したらどうなるの？ | — | Slack での動き | In Slack |
| 1 | 👀 は何？ | 受け取って処理している | 投稿がルールに合うと 👀 が付き、終わると外れて、スレッドに返信が付きます。 | When a post matches a rule, 👀 appears; when it's done, 👀 goes away and the reply shows up in the thread. |
| 2 | 続きはどう頼むの？ キーワードは要るの？ | スレッドの返信は同じルールで続く | スレッドに返信すると、同じルールで続きます。キーワードは要りません。AI エージェントは前のやりとりを覚えています。 | Reply in the thread to continue with the same rule — no keyword needed. The AI agent remembers the conversation. |

| 要素 | 日本語 | 英語 |
|---|---|---|
| 最後の CTA | README を読む | Read the README |

### 初めて開くとき

入れたあと、初めて開いた人が警告で止まったときに読む場所。決めたあとの人向けなので、ページの最後に置き、最初の画面の注意からリンクする（`#first-open`）。警告は「マルウェアが含まれていないことを検証できません」と書き、「ゴミ箱に入れる」が青い主ボタンなので、押し間違えないように最初の手順で「完了」を指す。

| 要素 | 訪問者の問い | 伝えたいこと | 日本語 | 英語 |
|---|---|---|---|---|
| 見出し | — | — | 初めて開くとき | Opening it the first time |
| 導入 | なぜ止められたの？ | 公証を受けていないから。想定どおりの動き | SockDock は Apple の公証を受けていないため、初めて開くと macOS が「“SockDock” は開いていません」と表示して止めます。 | SockDock is not notarized by Apple, so the first time you open it, macOS stops it with “SockDock” Not Opened. |
| 1 | どのボタンを押せばいい？ | 「ゴミ箱に入れる」ではなく「完了」 | 「完了」を押します（「ゴミ箱に入れる」は押さない）。 | Click Done (not Move to Trash). |
| 2 | どこで許可するの？ | システム設定のどこにあるか | システム設定の「プライバシーとセキュリティ」を開き、下のほうにある「このまま開く」を押します。 | Open System Settings → Privacy & Security, scroll down and click Open Anyway. |
| 3 | 毎回するの？ | もう一度確認が出る。次からはそのまま開ける | 確認が出たら「開く」を押します。次からはそのまま開けます。 | When asked again, click Open. From then on it opens normally. |
| 安全について | 許可して大丈夫？ | 許可されるのは SockDock だけ。Homebrew がファイルを確かめている | 許可されるのは SockDock だけで、Mac の保護はそのまま働きます。Homebrew は、ダウンロードしたファイルが tap に記録されたハッシュと一致することを確かめてから入れます。 | This allows SockDock only; the rest of your Mac's protection stays on. Homebrew checks the download against the checksum recorded in the tap before installing it. |
| リンク | 公式の説明は？ | Apple の説明 | Apple サポート「Mac でアプリを安全に開く」（support.apple.com/ja-jp/102445） | Apple Support: Safely open apps on your Mac (support.apple.com/en-us/102445) |

- （提案）`xattr` で quarantine を外すコマンドは載せない。Gatekeeper のチェックを外す方法を訪問者に勧めることになるため。
- 参考: 署名のない Mac アプリの配布ページで、ダウンロードの横ではなく独立した「インストールのご案内」の節に、理由・番号付きの手順・安全面の注意・Apple のサポートへのリンクを置いている例（https://koyasi777.com/mozkey/）。

### フッター

- GitHub · README · MIT License · Security
- SockDock is not affiliated with, created by, or supported by Slack Technologies, LLC.（日本語でも英語のまま。README と同じ文）

## 7. 言葉

アプリの画面と同じ言葉を使います（ia.md §9）。ルールの 3 つは「チャンネル・反応する投稿・実行するもの」で、「動かすもの」とは言わない（見出しの動詞「動かす」はそのまま）。README の CLI の言葉（route / handler）は LP では使いません。LP の訪問者が最初に触るのはアプリだからです。

| LP の言葉（英 / 日） | 使わない言葉 |
|---|---|
| rule / ルール | route, handler |
| Slack app / Slack アプリ | bot, `@SockDock` |
| AI agent / AI エージェント（実行するものの種類として書くときだけ「いまは Claude Code」と添える） | Claude handler |
| program / プログラム | command handler |
| which posts / 反応する投稿 | trigger, regex（見出しや例では使わない） |

## 8. 決めること

- **公開する時期:** Homebrew での配布ができてから公開する。2026-11-14 時点で、tap（`iwamatsu0430/homebrew-tap`）はあるが、リリースはまだない。リリースがないと LP の `brew install` は失敗する。
- **brew のコマンド:** Homebrew の配布（コミット 81434b2）に合わせて、メニューバーのアプリの `brew install --cask iwamatsu0430/tap/sockdock` にした。CLI（`brew install --formula iwamatsu0430/tap/sockdock`）は README に任せる。

## 9. 読み手による評価

文言を、製品を知らない読み手（サブエージェント）4 人に読ませた結果と、それを受けて変えたこと。読み手は役柄を演じた AI で、実際の利用者ではない。

### 2026-11-14 見出し「Post in Slack. Your rules run your Mac.」＋ 3 段階

読み手: 英語の開発者・Slack を管理するエンジニアリングリード・日本語の開発者・エンジニアではない同僚。全員「Slack が引き金で Mac が動く」は読み取ったが、入れる・許可すると決めた人はいなかった。

| 問題 | 変えたこと |
|---|---|
| 見出しを「ルールが Mac を管理する」（MDM など）と誤読した。日本語では「決めたとおり」を誰がどこで決めるのかがわからない | 見出しを具体例 2 つにした |
| AI エージェントが出てくるのが遅い | 見出しの 1 行目に Claude Code を入れた |
| 誰が動かせるのかがわからない（全員の疑問の 1 位） | ルールで決める 3 つの 1 つ目で答えた |
| ルールの実例がない。「どの投稿で」「何をしてよいか」があいまい | 3 つの項目それぞれに例を付けた |
| 「外から入る口は開けません」が根拠のない安心の言葉に見える | 「ポートを開けたり、公開 URL を用意したりする必要はありません」と、しなくてよいことを具体的に書いた |

### 2026-11-14 見出し「例 2 つ」＋ ルールで決める 3 つ（ページ全体）

読み手は前回と同じ 4 人で、ページ全体を見せた。全員が見出しの時点で「投稿の言葉で何かが動く」を中程度の確信で読み取り、「Mac を管理する」という誤読は消えた。開発者 2 人は「公式の Slack 連携はクラウド、これは自分の Mac」という違いを自分の言葉で言えた。スレッドの絵は「いちばんわかりやすい」と言われた。インストールを決めた人はまだいない（開発者 2 人は GitHub を開く、リードは条件付きで保留、非エンジニアは条件付きで OK）。

| 問題 | 誰が |
|---|---|
| 3 つ目の項目の「ファイルの読み取りだけを許可」と、スレッドの絵の「直して → 直しました」が食い違う | 4 人全員 |
| スレッドの「直して」には「ask」が付いていないのに動いた。「当てはまらない投稿では、何も起きません」と合わない（スレッドの返信は同じルールで続くことが書かれていない） | 英語の 2 人 |
| 「遊ぼう → ゲームを起動」が、どこで・誰のために起動するのかわからない。ふざけて見える、何の違いを示す例かわからない | 4 人中 3 人 |
| 「誰でもルールを使えます」がいちばん怖い行。投稿者で絞れるのか、ゲストや外部の人も含むのか | 4 人全員 |
| チャンネルの人の依頼が、1 人の Claude のアカウントで動く。規約上よいのか、誰の利用量か | 3 人 |
| Mac がスリープしている・閉じているときはどうなるか | 3 人 |
| 「設定は SockDock が入れてくれます」の「設定」、「トークンを 2 つ」が何か | 日本語の開発者 |
| 「ツールにも届きます」が直訳っぽい | 日本語の開発者 |

2 回目を受けて変えたこと: 家で使うものと決めたので、会社の管理者の疑問は扱わない（§2）。1 つ目の例のチャンネルを `#個人開発` にした。スレッドの絵を `#個人開発` と `#家族` の 2 つにし、1 つ目の続きを読み取りだけで答えられる質問にして、食い違いを消した。

### 2026-11-14 家で使う場面（`#個人開発` と `#家族` のスレッド）

読み手: Hacker News から来た開発者、家に Mac mini を置いて個人開発をする開発者（英語・日本語）、その家族でエンジニアではないパートナー。全員が見出しの時点で「投稿の言葉で Mac の何かが動く」を中程度の確信で読み取った。開発者 3 人は「サーバーが要らず、自分の Mac で動く」をいちばんの魅力に挙げた。「読み取りだけ」と「直しました」の食い違いは指摘されなくなった。インストールを決めた人はいない（開発者 3 人は GitHub を開く、パートナーは「あなた専用のチャンネルだけならいい」）。

| 問題 | 誰が |
|---|---|
| 「ゲームを起動」が、どこで（誰の画面で）起動するのかわからない。画面のない Mac mini ならなおさら | 4 人全員 |
| 自分で書いたスクリプトやコマンドも動かせるのかが書かれていない。家の開発者 2 人は、エアコンや明かりを動かしたいのに、できるかがわからない | 開発者 3 人 |
| 誰でも動かせる。子どもも Claude Code を動かせてしまう。人ごとに絞れるか | 4 人全員 |
| スレッドの 2 回目の投稿は「ask」なしで動いた。項目 2 と合わない | 家の開発者 2 人 |
| 家族の投稿でも自分の Claude の利用枠が減るのか、規約上よいのか | 家の開発者 2 人 |
| アプリの形（GUI か CLI か）、ルールをどこで作るか、スリープ中の動き | 開発者 3 人 |
| 仕事用か家用か判断できない。家族の例でかえって誰向けかわからなくなった | Hacker News の開発者 |
| 「ルールを使えます」が不自然（実際は「動かせる」）。「の上で」「手元のファイルとツールのまま」「届きます」が直訳調。「設定」が何かわからない。「動かすもの」と「実行するもの」で言葉が揺れている | 日本語の開発者 |
| 👀 の意味が書かれていない | 2 人 |

3 回目を受けて変えたこと: 何に使うか（使いみち）がわからないという指摘は扱わない。使い方の詳細（Slack アプリの設定、2 つのトークン、ルールを作る場所、実行できる 2 種類、👀 の意味、スレッドの続き）を「3 つの手順で始める」の補足と「Slack での動き」にまとめた。日本語の言い回し（「ルールを動かせます」「自分の Mac で動きます」「手元のファイルをそのまま使う」）と、「実行するもの」への言葉のそろえを直した。
