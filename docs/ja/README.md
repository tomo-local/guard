# ai-guard-tracker

[English](../../README.md) | **日本語**

AI エージェント（Claude Code 等）が実行する Bash コマンドを、事前定義したポリシーで
ALLOW / DENY / CONFIRM に判定する guardrail CLI `guard`。

## 目次

- [概要](#概要)
- [特徴](#特徴)
- [クイックスタート](#クイックスタート)
- [コマンド](#コマンド)
  - [`guard init`](#guard-init)
  - [`guard check`](#guard-check)
  - [`guard record`](#guard-record)
  - [`guard lint`](#guard-lint)
  - [`guard report`](#guard-report)
- [設定](#設定)
- [ポリシーの書き方](#ポリシーの書き方)
  - [フラグ単位のマッチ](#フラグ単位のマッチtoargsanyof)
  - [カスタムチェック](#カスタムチェックoptionscustomcheck)
- [既知の制限（v1）](#既知の制限v1)
- [開発](#開発)

## 概要

AIコーディングエージェントはユーザーに代わってBashコマンドを実行するが、
force-push や `rm -rf`、未レビューの `curl | sh` のように、黙って実行されては
困るコマンドもある。`ai-guard-tracker` は Claude Code の hook 機構に組み込む
小さなCLI `guard` で、事前にリポジトリへ書いておいたポリシーマニフェストに基づき、
コマンドごとに **ALLOW**（許可）・**DENY**（拒否）・**CONFIRM**（人間に確認）
のいずれかを判定する。

ポリシーの記述形式は [Istio の `AuthorizationPolicy`](https://istio.io/latest/docs/reference/config/security/authorization-policy/)
を参考にした Kubernetes 風マニフェスト。Istio の `AuthorizationPolicy` を書いた
ことがあれば見覚えのある形になっているはず。

`guard init` / `guard check` / `guard record` / `guard lint` / `guard
report` をすべて実装している。

## 特徴

- **ポリシー駆動の判定**: Claude Code の `PreToolUse` hook として評価され、
  コマンドが実行される前に `guard check` が ALLOW/DENY/CONFIRM
  （`allow`/`deny`/`ask`）を返す。
- **Istio風マニフェスト**: 1ツール・1アクション（ALLOW・DENY・CONFIRMのいずれか）・
  1ファイルを基本とし、`.guard/policies/<tool>/` にまとめる。
- **CONFIRMは修飾子ではなく同格のaction**: `spec.action: CONFIRM` はALLOW/DENYと
  並ぶenumの値そのもので、「DENYなのに確認で通せる」という組み合わせ自体が書けない
  （1リソースは1つのactionしか持てないため）。
- **フラグ単位のマッチ**（`to.args.anyOf` / `to.args.noneOf`）: 例えば `git push` と
  `git push -f` を区別できる。
- **外部スクリプトへの委譲**（`options.customCheck`）: 判定の一部を外部スクリプトに
  委譲し、その終了コードで判定を上書きできる（Istio の `action: CUSTOM` に相当）。
- **複合コマンド対応**: `&&`/`;`/`|`/サブシェル/コマンド置換を実際のシェルAST
  パーサーで分解し、最も厳しい判定（`DENY > CONFIRM > ALLOW`）が全体に適用される。
- **生のまま記録するログ**: 判定結果（`guard check` → `.guard/logs/check/YYYY-MM-DD.jsonl`）
  と実際の実行結果（`guard record` → `.guard/logs/record/YYYY-MM-DD.jsonl`）の両方を
  要約せずJSON Linesで、UTC日付ごとに1ファイルへ分けて記録する。
- **環境変数で設定可能**（`GUARD_POLICIES_DIR`、`GUARD_LOG_PATH`、
  `GUARD_RECORD_LOG_PATH`、`GUARD_CONFIG_PATH`）。配布したバイナリに毎回フラグを渡す必要が無い。

## クイックスタート

```bash
go build -o bin/guard .
./bin/guard init
```

`guard init` は `.guard/policies/`（空の場合のみスターターポリシーを投入）と
`.guard/logs/check/`・`.guard/logs/record/` を用意し、`.claude/settings.json` に追加すべき hook スニペットを
表示する。フルの設定方法は [`../hooks-setup.md`](../hooks-setup.md)（英語）/
[`hooks-setup.md`](hooks-setup.md)（日本語）を参照。

hookを登録したら、直接試せる:

```bash
guard check "git push --force"
# DENY git push --force
# reason: git push --force force-pushes and can overwrite remote history.
# next: Suggest a non-destructive alternative, or ask the user to run it themselves.
```

## コマンド

### `guard init`

新しいプロジェクトで `.guard/policies/`・`.guard/logs/`・`.guard/config.yaml`
を用意する。

```bash
guard init
```

- `.guard/policies/` が空（ポリシーが1つも無い）場合のみ、このCLIに同梱された
  スターターポリシー（`git`/`rm`/`curl` 向けの5ファイル、`go:embed` でバイナリに
  埋め込み済み）を書き込む。既にポリシーがある場合は**一切上書きしない**
- `.guard/logs/check/`・`.guard/logs/record/`（ログの出力先ディレクトリ。それぞれ
  UTC日付ごとに `YYYY-MM-DD.jsonl` が1つ作られる）も作成する
- **`.guard/config.yaml`（このCLI自身のプロジェクト設定、Bashポリシーとは別物）も
  用意する**。今のところ設定項目は `guard report --format duckdb` を使うかどうかと、
  使う場合のデータベースファイルの場所のみ:
  - `-duckdb`/`-duckdb-path` のいずれかが渡された場合は、何も聞かずにその内容を
    保存する（スクリプト/非対話実行向け）
  - そうでなく、`.guard/config.yaml` がまだ存在しない場合は、標準入出力で対話的に
    質問する（`[y/N]`、続けてデフォルト付きのパス入力）。空/EOFの回答は安全側の
    「いいえ」（DuckDB無効のまま）にフォールバックするため、非対話的なstdinでも
    ハングしたり意図せず有効化されたりしない
  - `.guard/config.yaml` が既に存在していて、どちらのフラグも渡されなかった場合は
    そのまま変更しない -- `guard init` を再実行しても聞き直されない
- `.claude/settings.json` には**自動で書き込まない**（既存設定を壊すリスクを避けるため）。
  追記すべき hook JSON スニペットを標準出力に表示するだけ
- `-rules`/`-log`/`-record-log`/`-config` の作成先解決ルールは `guard check`/`guard lint`/
  `guard report` と共通（後述の[設定](#設定)参照）

### `guard check`

Bash コマンド文字列を評価し、ALLOW/DENY/CONFIRM を返す。2つの入力モードを自動判別する。

```bash
# CLI モード: 引数でコマンドを直接渡す
guard check "git push --force"

# hook モード: Claude Code の PreToolUse hook payload を stdin から受け取る
echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | guard check
```

- **CLI モード**: 人間向けテキストを stdout に出力。exit code は `0=ALLOW` / `1=DENY` / `2=CONFIRM`。
- **hook モード**: Claude Code 互換の JSON（`hookSpecificOutput.permissionDecision`:
  `allow`/`deny`/`ask`、`permissionDecisionReason` に理由と次アクション）を出力し、常に exit 0。
  `tool_name` が `Bash` 以外の場合は `{}` を返し何もしない（スコープ外）。

コマンドはまずツールごとのポリシーに照らされる。どのポリシーもマッチしなかった場合にだけ、
プロジェクトの manifest（`.guard/manifest.yaml`、[GuardManifest](manifest-schema.md)を参照）が、
コマンドが触るファイルパスで判定する（たとえば `/cmd` 配下は拒否）。

判定結果は `.guard/logs/check/YYYY-MM-DD.jsonl`（JSON Lines、1行1判定で追記のみ、
UTC日付ごとに1ファイル）に記録される。

`guard check` を `PreToolUse` hook として登録することで、実際に効果を持つように
なる。詳細は [`../hooks-setup.md`](../hooks-setup.md) を参照。

### `guard record`

Claude Code の **PostToolUse** hook payload を stdin から受け取り、Bash コマンドが
実際に何を行ったか（コマンド・生の`stdout`/`stderr`（要約せずそのまま）・中断されたか）
を `.guard/logs/record/YYYY-MM-DD.jsonl`（UTC日付ごとに1ファイル）に追記する。
CLIモードは無く、hookとして使うことのみを想定している。

```bash
echo '{"tool_name":"Bash","tool_input":{"command":"echo hi"},"tool_response":{"stdout":"hi\n"}}' | guard record
```

- `tool_name != "Bash"` は no-op（何もしない）
- **常に exit 0**（recordが失敗してもAIの処理を止めない。ログ書き込み失敗はstderrに
  warningを出すだけ）
- Claude Code が公開している Bash の `tool_response` には終了コードのフィールドが無い
  （`stdout`/`stderr`/`interrupted`/`isImage`のみ）ため、`guard record` は
  成功/失敗フラグを捏造しない

`PostToolUse` → `guard record` として登録する。詳細は
[`../hooks-setup.md`](../hooks-setup.md) を参照。

### `guard lint`

`.guard/policies/` 配下の全ポリシーマニフェスト（サブディレクトリも再帰的に読み込む）を検証する。
問題はすべて1回の実行でまとめて報告され、ファイルごとにグループ化されて最後にサマリー行が付く。
パースに失敗したファイルも、他の問題と同じように報告される。出力はターミナルでは色付けされる
（`-color always|never|auto`。`NO_COLOR` も尊重する）。

```bash
guard lint
guard lint -rules ./some/other/dir
```

exit code は `0=エラー無し`（warningのみなら0のまま出力はされる）、`1=エラーあり`。

### `guard report`

`.guard/logs/check/` と `.guard/logs/record/` 配下の全ファイルを集計してサマリーを
出力する。action/mode別件数、tool別の内訳、DENY/CONFIRMが多い `tool`+subcommand
の組み合わせ、`options.customCheck` の実行/タイムアウト/override件数、日次
タイムライン、record側の中断件数などを含む。

```bash
guard report                                 # 自己完結HTMLをstdoutへ（デフォルト）
guard report --format md                     # Markdownサマリーをstdoutへ
guard report --format json --out report.json # 機械可読な形式でファイルへ出力
guard report --format duckdb                 # guard initで設定したDBパスへロード
```

- **`-format`** は `md` / `json` / `html`（**デフォルト**） / `duckdb` のいずれか。
  `md`/`json`/`html` は同じデータを形式だけ変えて出力する -- PRやチャットに
  貼るなら `md`、スクリプト処理するなら `json`、外部リクエスト無しでオフライン
  共有できるページが欲しいなら `html`、好きな方を選べる。いずれも `-out` を
  省略すればstdoutへ、指定すればそのファイルへ書き出す。
- `-format duckdb` を指定する場合は、PATH上の `duckdb` CLI（例: `brew install
  duckdb`）に加えてデータベースのパスが必要 -- `-out` が指定されていればそれを、
  無ければ `.guard/config.yaml`（`guard init` が用意する。後述）の
  `duckdb.path` を使う。各ディレクトリ配下の全 `*.jsonl` 日付ファイルの内容から、
  そのデータベースファイルに `checks`/`records` テーブルを（差分ではなく）
  毎回フルリロードで作り直し、DuckDB自身がレンダリングした月別×action別の
  集計結果を表示する。生成された `.duckdb` ファイルはそのまま残るので、
  以降は自分で自由にSQLを投げられる（`duckdb <path>`）-- guardが唯一の
  クエリ手段になろうとはしない。
- `-log`/`-record-log`/`-config` の解決順は他コマンドと同じ（`-flag` > 環境変数 >
  デフォルト、[設定](#設定)参照）。
- どちらのログディレクトリも事前に存在しなくてよい -- 無い/空の場合はそのことを示す
  レポートを出力するだけでエラーにはしない（未知の `-format`、`-format duckdb`
  で `-out` も設定済みパスも無い場合、`duckdb` CLIが無い場合などの
  usage/runtimeエラー以外はexit code常に`0`）。
- v1では日付範囲やtool/actionでの絞り込みは未対応。
  [既知の制限（v1）](#既知の制限v1)を参照。

## 設定

`-rules`/`-log` はCLIとして配布・インストールした場合など、プロジェクトごとに毎回
指定したくない場合に環境変数でも上書きできる。優先順位は **`-flag` > 環境変数 > デフォルト**。

| 環境変数 | 対象コマンド | 対象フラグ | デフォルト |
|---|---|---|---|
| `GUARD_POLICIES_DIR` | `init`/`check`/`lint` | `-rules` | `.guard/policies` |
| `GUARD_LOG_PATH` | `init`/`check`/`report` | `-log` | `.guard/logs/check`（UTC日付ごとに `YYYY-MM-DD.jsonl` を1つ；`init`がこのディレクトリを作成） |
| `GUARD_RECORD_LOG_PATH` | `init`/`record`/`report` | `-log`（`record`）/ `-record-log`（`init`、`report`） | `.guard/logs/record`（UTC日付ごとに `YYYY-MM-DD.jsonl` を1つ） |
| `GUARD_CONFIG_PATH` | `init`/`report` | `-config` | `.guard/config.yaml`（`init`が書き込み、`report`は読むだけ） |
| `GUARD_MANIFEST_PATH` | `check`/`lint` | `-manifest` | `.guard/manifest.yaml`（任意。[GuardManifest](manifest-schema.md)を参照） |
| `GUARD_DB_DIR` | `check`/`lint` | `-db` | `.guard/db`（任意の `connections.yml` と `policy.yml`。[データベース関連ファイル](db-schema.md)を参照） |

```bash
GUARD_POLICIES_DIR=./some/other/dir guard lint
```

> **破壊的変更**: `GUARD_LOG_PATH`/`GUARD_RECORD_LOG_PATH`（および
> `-log`/`-record-log`）は以前、単一の `.jsonl` **ファイル**
> （`.guard/logs/check.jsonl`）を指していましたが、現在はUTC日付ごとに
> `YYYY-MM-DD.jsonl` を1つ持つ**ディレクトリ**を指すようになりました。
> どちらかが旧ファイルパスに設定されている場合は、ディレクトリパス
> （例: `.guard/logs/check`）に更新してください -- 旧ファイルパスのままだと
> `guard check`/`guard record` はログ書き込みがサイレントに止まり、
> `guard init` は完全に失敗します（既存ファイルに対する `mkdir` は必ず
> 失敗するため）。

## ポリシーの書き方

ポリシーは [Istio の `AuthorizationPolicy`](https://istio.io/latest/docs/reference/config/security/authorization-policy/)
を参考にした Kubernetes 風マニフェスト。1ツール・1アクション・1ファイルとし、同じツールの
ポリシーは `.guard/policies/<tool>/` ディレクトリにまとめる。

> このセクションは概要的な説明。すべてのフィールド・型・検証ルール・
> `guard lint` のチェック内容を含む完全な仕様は
> [**docs/ja/policy-schema.md**](policy-schema.md) を参照。

```yaml
# .guard/policies/git/allow.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-allow
spec:
  target:
    module: permission   # v1では "permission" 固定
    tool: git
  action: ALLOW           # ALLOW | DENY | CONFIRM のいずれか1つ。このリソース内の全ruleに共通
  rules:
    - to:
        label: read                                    # 任意、自由記述。マッチングには影響しない
        functions: [log, status, diff, show, branch, fetch]
```

```yaml
# .guard/policies/git/write-confirm.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-write-confirm
spec:
  target:
    module: permission
    tool: git
  action: CONFIRM
  rules:
    - to:
        label: write
        functions: [add, commit, pull, merge]    # 省略するとツール全体へのcatch-all
      message:        # 任意
        reason: "{cmd} is a write operation."
        next: "Confirm with the user before proceeding."
    - to:
        label: write
        functions: [push]
        args:
          noneOf: ["-f", "--force"]    # push -f/--force は push-force-deny.yaml の担当
      message:
        reason: "{cmd} is a write operation."
        next: "Confirm with the user before proceeding."
```

`message.reason`/`message.next` は `permissionDecisionReason` としてそのまま AI（Claude）に
渡される文字列なので **英語で書く**。`{cmd}` は唯一サポートされるプレースホルダで、実際にマッチ
したコマンド（例: `git add -A .`）に置き換えられる。

- **action はリソース単位で1つだけ**、かつ **ALLOW/DENY/CONFIRMの3値**（Istio の
  ALLOW/DENYコアに対して、CONFIRMを「ALLOWに確認を挟む」という派生状態ではなく
  同格のenum値として追加している）。以前の設計ではCONFIRMをALLOW+confirmフラグから
  導出していたが、それだと「DENY+そのフラグ」という書けるが矛盾する組み合わせが
  生まれ、専用のlintルールで弾く必要があった。actionを3値の同格な値にすることで、
  そもそも1つのリソースが複数のactionを同時に持てなくなり、その組み合わせ自体が
  書けなくなる。1ツールに複数の action が必要ならリソースファイルを分ける（例:
  `git/allow.yaml`、`git/write-confirm.yaml`、`git/destructive-deny.yaml`）。
  同じ action 内であれば `rules[]` を増やして1ファイルにまとめてよい。
- 複数のポリシーが同じコマンドにマッチした場合、および `&&`/`;`/`|` 等で分解された
  複数コマンドの判定が割れた場合は、**DENY > CONFIRM > ALLOW** の最も厳しい方が勝つ。
- どのポリシーにも一致しない場合のデフォルトは **CONFIRM**（内部フォールバックであり、
  YAML上の `action` としては書けない）。
- `functions` を省略すると、そのツールの全呼び出しに対する catch-all ルールになる
  （`rm`/`curl` のようにサブコマンドを持たないフラットなコマンド向け）。
- `to.label` は読み手・レビュアー向けの自由記述の任意フィールド（例:
  `write`、`destructive`、`network`、あるいは好きな文字列）。**マッチングには
  一切関与しない** —— マッチングは `functions`/`args` だけで決まる。
  チェックログ（`.guard/logs/check/*.jsonl` の `commands[].label`）に記録され、
  可読性のために使われる。

### フラグ単位のマッチ（`to.args.anyOf` / `to.args.noneOf`）

`to.args.anyOf` を付けると、そのルールは引数（argv）に指定したトークンのいずれかを含む
場合にのみマッチする。`to.args.noneOf` を付けると、指定したトークンを1つも含まない
場合にのみマッチする（どちらも正規表現やglobは使わず、トークンの完全一致のみ）。

どのコマンドも、判定するポリシーはちょうど1つでなければならない。同じ tool の2つのポリシーが
あるコマンドに両方マッチし得る場合、`guard lint` はエラーにする。そのため
「`git push` 単体は CONFIRM、`git push -f`/`--force` は DENY」は、条件が重ならない2つの
リソースで書く。`push` のruleに `args.noneOf: ["-f", "--force"]` を付けた
`git/write-confirm.yaml` と、`args.anyOf: ["-f", "--force"]` を付けた
`git/push-force-deny.yaml` である。重なるポリシーがlintをすり抜けた場合は、`check` は
strictest-wins（DENY > CONFIRM > ALLOW）にフォールバックする。

```yaml
# .guard/policies/git/push-force-deny.yaml
spec:
  target:
    module: permission
    tool: git
  action: DENY
  rules:
    - to:
        label: destructive
        functions: [push]
        args:
          anyOf: ["-f", "--force"]
      message:
        reason: "{cmd} force-pushes and can overwrite remote history."
        next: "Suggest a non-destructive alternative, or ask the user to run it themselves."
```

### ブランチ単位のマッチ（`to.branch`）

manifest の `repositoryGate` がリポジトリごとにブランチへラベル（`main`、`protected`）を付け、
ruleの `to.branch` がそのラベルを使う。これにより、「保護ブランチは変更させない」を、
ポリシーごとにブランチ名を書かずに表現できる。[ブランチのラベル](manifest-schema.md#repositorygateブランチのラベル)と
[`to.branch`](policy-schema.md#toマッチ条件)を参照。

```yaml
# .guard/manifest.yaml
spec:
  repositoryGate:
    repose:
      - name: owner/name
        main: main
        protected: [main, staging, "docs/*"]
```

```yaml
# .guard/policies/git/protected-deny.yaml
spec:
  target: { module: permission, tool: git }
  action: DENY
  rules:
    - to:
        functions: [push, commit, merge, reset, rebase]
        branch: { anyOf: [protected] }
```

### データベースクライアント（`.guard/db/`）

`psql` と `mysql` は、ツールごとのポリシーと同じ書き方の SQL ルールで制御する（`functions` に `DROP`、`SELECT` などの
文のキーワードを書く）。`.guard/db/connections.yml` が環境を定義してラベルを付け、`.guard/db/policy.yml` がラベルごとの
ルールを書き、manifest の `sqlGate` が環境に関係なく効くルールを持つ。[データベース関連ファイル](db-schema.md)を参照。

```yaml
# .guard/db/connections.yml
spec:
  environments:
    - name: production
      labels: [prod]
      cli:
        hosts: ["*.prod.example.com"]
```

```yaml
# .guard/db/policy.yml
spec:
  rules:
    - to:
        labels: [prod]
        functions: [DROP, TRUNCATE]
      action: DENY
    - to:
        labels: [prod]
        functions: [SELECT]
      action: ALLOW
    - to:
        labels: [prod]
      action: CONFIRM
```

### カスタムチェック（`options.customCheck`）

Istio の `action: CUSTOM`（外部認可委譲）に相当する仕組み。マッチしたルールに紐づけて
スクリプト/インラインコマンドを実行し、終了コードで action を上書きできる。

```yaml
# ベースは DENY（スクリプトが安全だと示さない限り拒否する、安全側のデフォルト）
spec:
  action: DENY
  rules:
    - to:
        label: write
        functions: [push]
      options:
        customCheck:
          path: "./scripts/check-push.sh"   # path か command のどちらか一方のみ指定
          args: ["--staged"]
          timeoutSeconds: 10                 # デフォルト10秒。超過時はベースのaction（DENY）にフォールバック
          when:
            - key: exit
              value: 0
              action: CONFIRM   # 安全と判定されても一応人間に確認してもらう
            # when のどれにも一致しない場合（exit!=0 等）は spec.action=DENY にフォールバック
```

**`options.customCheck` を設定した場合の優先順位**: マッチした `when` エントリは
常にリソース自身の `action` を上書きする（`DENY`/`CONFIRM` のリソースを素の `ALLOW`
に格下げすることさえある）。上書きが発生しないのは `customCheck` が設定されていない
場合、実行されなかった場合、または `when` のどれにもマッチしなかった場合（タイムアウト
含む）のみで、その場合はリソース自身の `action` がそのまま使われる。

> **セキュリティ注意**: `customCheck` のスクリプトは `.guard/policies/` と同じ信頼境界
> （リポジトリにコミットされ、レビューされたもの）で実行される前提。信頼できないスクリプト
> を参照しないこと。

## 既知の制限（v1）

- manifest のパスマッチはコマンド内の `cd` を追跡せず、パスに見える引数は何でもパスとして扱う。
  [GuardManifest](manifest-schema.md#既知の制限)を参照。
- ブランチ条件（`to.branch`）は、`guard` を実行したディレクトリで `git remote get-url origin` を実行して
  リポジトリを、コマンド（`git push` の refspec、`git branch -d <name>`）または現在のブランチからブランチを決める。
  `git push --all/--mirror` と `git -C <path>` は追跡しない。
- データベースの接続先と SQL（`.guard/db/`、`sqlGate`）はコマンドラインだけから読む（`psql`/`mysql` のフラグ、URI、
  位置引数のデータベース名、`-c`/`-e` のテキスト）。環境変数、`~/.pgpass`、`-f` のファイル、標準入力は読まない
  （その SQL は `UNKNOWN` になる）。文の最初の語だけを見る。
- 対象スコープは **Bash コマンドのみ**。
- サブコマンド（`functions`）の判定は「先頭の `-` で始まらない最初の引数」というヒューリスティック。
  `git -C <path> push` のように値を取るフラグの直後の引数を誤って function とみなす場合がある。
- マッチ条件の粒度は `functions` × `args.anyOf`/`args.noneOf` まで（`to.label` は自由記述の
  説明用フィールドであり、マッチングには関与しない）。`args` はトークンの
  完全一致のみ（正規表現/glob不可、値を取るオプションの引数部分まではパースしない）。
  それ以上の複雑な判定が必要な場合は `options.customCheck` でスクリプト側に委譲する。
- コマンド置換（`$(...)`）の中の呼び出しも AST walk で評価対象になるが、リテラルでない
  引数（変数展開など）は元のシェル構文がそのまま文字列化されるため、`functions` 一致の
  対象にはならずその呼び出しは catch-all/デフォルトにフォールバックする。
- `guard report` には日付範囲やtool/actionでの絞り込みが無く、checkログと
  recordログのエントリも結合しない（Claude Codeの`PreToolUse`ペイロードには
  `tool_use_id`が無く、確実に結合できるキーが無いため）-- 各ログは独立に集計する。
  `-format duckdb`で作られる`checks`/`records`テーブルも同様で、両者を
  `JOIN`できる共通キーは無い。
- checkログの`commands[]`にある`function`フィールド（`guard report`の
  tool+function集計に使う）は、このフィールドが追加された後に記録された
  エントリにのみ存在する。それより古いログ行には単に存在しない。

## 開発

```bash
go build ./...
go vet ./...
go test ./...
```
