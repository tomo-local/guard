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

`guard init` / `guard check` / `guard record` / `guard lint` を実装している。
残っているのは `guard report` のみ（詳細は `tmp/remaining-plan.md` 参照）。

## 特徴

- **ポリシー駆動の判定**: Claude Code の `PreToolUse` hook として評価され、
  コマンドが実行される前に `guard check` が ALLOW/DENY/CONFIRM
  （`allow`/`deny`/`ask`）を返す。
- **Istio風マニフェスト**: 1ツール・1アクション（ALLOW・DENY・CONFIRMのいずれか）・
  1ファイルを基本とし、`.guard/policies/<tool>/` にまとめる。
- **CONFIRMは修飾子ではなく同格のaction**: `spec.action: CONFIRM` はALLOW/DENYと
  並ぶenumの値そのもので、「DENYなのに確認で通せる」という組み合わせ自体が書けない
  （1リソースは1つのactionしか持てないため）。
- **フラグ単位のマッチ**（`to.args.anyOf`）: 例えば `git push` と `git push -f`
  を区別できる。
- **外部スクリプトへの委譲**（`options.customCheck`）: 判定の一部を外部スクリプトに
  委譲し、その終了コードで判定を上書きできる（Istio の `action: CUSTOM` に相当）。
- **複合コマンド対応**: `&&`/`;`/`|`/サブシェル/コマンド置換を実際のシェルAST
  パーサーで分解し、最も厳しい判定（`DENY > CONFIRM > ALLOW`）が全体に適用される。
- **生のまま記録するログ**: 判定結果（`guard check` → `.guard/logs/check.jsonl`）
  と実際の実行結果（`guard record` → `.guard/logs/record.jsonl`）の両方を
  要約せずJSON Linesで記録する。
- **環境変数で設定可能**（`GUARD_POLICIES_DIR`、`GUARD_LOG_PATH`、
  `GUARD_RECORD_LOG_PATH`）。配布したバイナリに毎回フラグを渡す必要が無い。

## クイックスタート

```bash
go build -o bin/guard .
./bin/guard init
```

`guard init` は `.guard/policies/`（空の場合のみスターターポリシーを投入）と
`.guard/logs/` を用意し、`.claude/settings.json` に追加すべき hook スニペットを
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

新しいプロジェクトで `.guard/policies/`・`.guard/logs/` を用意する。

```bash
guard init
```

- `.guard/policies/` が空（ポリシーが1つも無い）場合のみ、このCLIに同梱された
  スターターポリシー（`git`/`rm`/`curl` 向けの5ファイル、`go:embed` でバイナリに
  埋め込み済み）を書き込む。既にポリシーがある場合は**一切上書きしない**
- `.guard/logs/`（ログの出力先ディレクトリ）も作成する
- `.claude/settings.json` には**自動で書き込まない**（既存設定を壊すリスクを避けるため）。
  追記すべき hook JSON スニペットを標準出力に表示するだけ
- `-rules`/`-log` の作成先解決ルールは `guard check`/`guard lint` と共通
  （後述の[設定](#設定)参照）

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

判定結果は `.guard/logs/check.jsonl`（JSON Lines、1行1判定で追記のみ）に記録される。

`guard check` を `PreToolUse` hook として登録することで、実際に効果を持つように
なる。詳細は [`../hooks-setup.md`](../hooks-setup.md) を参照。

### `guard record`

Claude Code の **PostToolUse** hook payload を stdin から受け取り、Bash コマンドが
実際に何を行ったか（コマンド・生の`stdout`/`stderr`（要約せずそのまま）・中断されたか）
を `.guard/logs/record.jsonl` に追記する。CLIモードは無く、hookとして使うことのみを
想定している。

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

```bash
guard lint
guard lint -rules ./some/other/dir
```

exit code は `0=エラー無し`（warningのみなら0のまま出力はされる）、`1=エラーあり`。

## 設定

`-rules`/`-log` はCLIとして配布・インストールした場合など、プロジェクトごとに毎回
指定したくない場合に環境変数でも上書きできる。優先順位は **`-flag` > 環境変数 > デフォルト**。

| 環境変数 | 対象コマンド | 対象フラグ | デフォルト |
|---|---|---|---|
| `GUARD_POLICIES_DIR` | `init`/`check`/`lint` | `-rules` | `.guard/policies` |
| `GUARD_LOG_PATH` | `init`/`check` | `-log` | `.guard/logs/check.jsonl`（`init`はこのパスの親ディレクトリを作成） |
| `GUARD_RECORD_LOG_PATH` | `record` | `-log` | `.guard/logs/record.jsonl` |

```bash
GUARD_POLICIES_DIR=./some/other/dir guard lint
```

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
        functions: [add, commit, push, pull, merge]    # 省略するとツール全体へのcatch-all
      message:        # 任意
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
  チェックログ（`.guard/logs/check.jsonl` の `commands[].label`）に記録され、
  可読性のために使われる。

### フラグ単位のマッチ（`to.args.anyOf`）

`to.args.anyOf` を付けると、そのルールは引数（argv）に指定したトークンのいずれかを含む
場合にのみマッチする（正規表現やglobは使わず、トークンの完全一致のみ）。たとえば
「`git push` 単体は CONFIRM、`git push -f`/`--force` は DENY」は、通常の
`git/write-confirm.yaml`（CONFIRM、args条件なし）と、`args.anyOf: ["-f", "--force"]`
を付けた別の DENY リソース（`git/push-force-deny.yaml`）の2つに分けて書く。`-f` が
付いていれば両方マッチし DENY が勝ち、付いていなければ DENY 側がマッチせず CONFIRM の
ままになる（既存の strictest-wins がそのまま効く）。

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

- 対象スコープは **Bash コマンドのみ**。
- サブコマンド（`functions`）の判定は「先頭の `-` で始まらない最初の引数」というヒューリスティック。
  `git -C <path> push` のように値を取るフラグの直後の引数を誤って function とみなす場合がある。
- マッチ条件の粒度は `functions` × `args.anyOf` まで（`to.label` は自由記述の
  説明用フィールドであり、マッチングには関与しない）。`args.anyOf` はトークンの
  完全一致のみ（正規表現/glob不可、値を取るオプションの引数部分まではパースしない）。
  それ以上の複雑な判定が必要な場合は `options.customCheck` でスクリプト側に委譲する。
- コマンド置換（`$(...)`）の中の呼び出しも AST walk で評価対象になるが、リテラルでない
  引数（変数展開など）は元のシェル構文がそのまま文字列化されるため、`functions` 一致の
  対象にはならずその呼び出しは catch-all/デフォルトにフォールバックする。

## 開発

```bash
go build ./...
go vet ./...
go test ./...
```
