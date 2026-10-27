# CommandGatePolicy スキーマ仕様

`.guard/policies/**/*.yaml` マニフェスト（`CommandGatePolicy` リソース）の
完全なフィールド仕様。概要的な説明は [README の「Writing
Policies」](../../README.md#writing-policies)（日本語版は
[docs/ja/README.md](../ja/README.md#ポリシーの書き方)）を参照。本ドキュメントは
`guard lint` が検証するすべてのルールを含む網羅的な仕様書。

## ファイル配置

- 1ファイル = 1つの `CommandGatePolicy` リソース = 1つのtool + 1つの `action`。
- ポリシーディレクトリ（デフォルト `.guard/policies`、
  `GUARD_POLICIES_DIR`/`-rules` で変更可）以下は再帰的に読み込まれるため、
  同じツールのポリシーはサブディレクトリにまとめるのが慣例:
  `.guard/policies/<tool>/<短い action/qualifier 名>.yaml`
  （例: `git/allow.yaml`, `git/write-confirm.yaml`, `git/destructive-deny.yaml`,
  `rm/deny.yaml`）。
- `.yaml`/`.yml` の両拡張子を読み込む。それ以外の拡張子は無視される。

## トップレベル構造

```yaml
apiVersion: ai-guard/v1       # 必須。厳密に "ai-guard/v1" と一致する必要がある
kind: CommandGatePolicy       # 必須。厳密に "CommandGatePolicy" と一致する必要がある
metadata:
  name: <string>              # 必須、空文字不可。kebab-case が慣例
spec:
  target:
    module: permission        # 必須。v1では厳密に "permission" と一致する必要がある
    tool: <string>             # 必須、空文字不可。例: "git", "rm", "curl"
  action: ALLOW | DENY | CONFIRM  # 必須。このいずれか1つのみ
  rules:                       # 必須。最低1件
    - <Rule>
    - <Rule>
    ...
```

`spec.action` はこのリソース内の **すべての** rule に適用される
（1つのリソースの中で複数のactionを混在させることはできない）。
複数必要な場合は複数のファイルに分ける（[複数ファイルの例](#複数ファイルの例)を参照）。
`ALLOW`/`DENY`/`CONFIRM` は1つのenumの同格な3値で、`ALLOW` の上に別途
「confirm修飾子」を重ねる仕組みではない。これは意図的な設計: 以前は
`CONFIRM` を `ALLOW` + confirmフラグから導出していたが、その結果
`DENY` + そのフラグという「書けるが矛盾する」組み合わせが生まれ、専用の
lintルールで後から弾く必要があった。`CONFIRM` を `spec.action` の同格な
値にすることで、そもそもその組み合わせを**書けなくしている**（1つの
リソースは1つの `action` しか持てないため）。

## Rule

```yaml
- to:
    label: <string>                                # 任意、自由記述
    functions: [sub, commands]                    # 任意
    args:
      anyOf: ["-f", "--force"]                     # 任意
      noneOf: ["-x"]                               # 任意（anyOf と noneOf の片方または両方）
  options:                                         # 任意
    customCheck:
      path: "./scripts/x.sh"
      command: "..."
      args: ["..."]
      timeoutSeconds: 10
      when:
        - key: exit
          value: 0
          action: ALLOW | DENY | CONFIRM
  message:
    reason: "..."
    next: "..."
```

### `to`（マッチ条件）

| フィールド | 型 | 必須 | 補足 |
|---|---|---|---|
| `to.label` | string | 任意 | 自由記述の説明用テキスト（例: `read`、`write`、`destructive`、`network`、その他何でも）。**マッチングには一切関与しない** —— マッチングは `functions`/`args` だけで決まる。チェックログ（`CommandLog.label`）に記録され、可読性のために使われる。 |
| `to.functions` | string配列 | 任意 | このruleが対象とするサブコマンド名（例: `[add, commit, push]`）。**省略するとcatch-all**になり、サブコマンドの概念がないフラットなコマンド（`rm`/`curl`等）のツール全体に適用される。 |
| `to.args` | オブジェクト | 任意 | 下記参照。 |
| `to.args.anyOf` | string配列 | `args`を設定する場合は `anyOf`/`noneOf` のどちらか | 空でない文字列。argvにこのトークンのいずれかが**リテラル一致**で含まれる場合にのみマッチする（正規表現/glob不可、値を取るオプションの引数部分のパースも行わない）。 |
| `to.args.noneOf` | string配列 | `args`を設定する場合は `anyOf`/`noneOf` のどちらか | 空でない文字列。argvにこのトークンが**1つも含まれない**場合にのみマッチする。`anyOf` と併用した場合は両方を満たす必要がある。より厳しい別のポリシーに道を譲るために使う（[1コマンド1ポリシー](#1コマンド1ポリシー)を参照）。 |

サブコマンドの検出方法: 先頭の `-` で始まらない最初の引数を「function」と
みなすヒューリスティック。値を取るフラグの直後の引数を誤って function と
みなす場合がある（例: `git -C <path> push` では `push` ではなく `<path>` が
functionと判定される）。[既知の制限](../../README.md#known-limitations-v1)を参照。

1つのリソース内の `rules[]` でのマッチ優先順位: 検出した function が
`functions` に含まれるルールがあればそれが勝つ。どれにも一致しなければ、
最初の catch-all ルール（`functions` 省略）が適用される。`args.anyOf`/`args.noneOf` を
設定している場合は、どちらの種類に対してもさらに絞り込む条件になる
（`args` 条件に一致しないルールは、`functions`/catch-all の一致にかかわらず
スキップされる）。

### `options`（任意の修飾子）

| フィールド | 型 | 必須 | 補足 |
|---|---|---|---|
| `options.customCheck` | オブジェクト | 任意 | [CustomCheck](#optionscustomcheck) を参照。 |

#### `options.customCheck`

Istio の `action: CUSTOM` に相当する仕組み。判定の一部を外部スクリプトや
インラインコマンドに委譲する。

| フィールド | 型 | 必須 | 補足 |
|---|---|---|---|
| `options.customCheck.path` | string | `path`/`command` のどちらか一方のみ | 実行可能なスクリプトへのパス。`args` と共に直接実行される。 |
| `options.customCheck.command` | string | `path`/`command` のどちらか一方のみ | インラインシェルコマンド。`sh -c "<command>" <args...>` として実行される。 |
| `options.customCheck.args` | string配列 | 任意 | `path` もしくは `command` に渡す追加の引数。 |
| `options.customCheck.timeoutSeconds` | int | 任意（デフォルト `10`） | `0`以上である必要がある。プロセスが時間内に終了しない場合は強制終了され、「`when` のどれにも一致しなかった」扱いになる（リソース自身の `spec.action` にそのままフォールバック）。 |
| `options.customCheck.when` | `When` の配列 | 任意 | プロセスの終了コードに対して順に評価され、**最初に一致したもの**が採用される。どれにも一致しない場合（タイムアウトや実行エラーを含む）、customCheckによる上書きは発生しない。 |

`When` エントリ:

| フィールド | 型 | 必須 | 補足 |
|---|---|---|---|
| `when[].key` | string | 必須 | v1では厳密に `"exit"` のみサポート（唯一のチェック対象キー）。 |
| `when[].value` | int | 必須 | マッチさせる終了コード。 |
| `when[].action` | string（固定語彙） | 必須 | `ALLOW`、`DENY`、または `CONFIRM`。 |

> **セキュリティ注意**: `customCheck` のスクリプト/コマンドは、ポリシー
> ファイル自身と同じ信頼境界（このリポジトリにコミットされ、レビューされた
> もの）で実行される前提。信頼できないスクリプトを参照しないこと。

### `message`（rule単位）

| フィールド | 型 | 必須 | 補足 |
|---|---|---|---|
| `message.reason` | string | 任意 | Claude Code の `permissionDecisionReason` としてそのままAIに送られる —— **英語で書く**。end-user向けのUIテキストとしては表示されない。 |
| `message.next` | string | 任意 | 同上。AIが次に取るべき行動（例: 「実行前にユーザーに確認する」）。 |

いずれのフィールドにも、実際にマッチしたコマンド文字列（例: `git add -A
.`）に置き換えられるプレースホルダ `{cmd}` を書ける。v1でサポートされる
唯一のプレースホルダ。

## 評価セマンティクス

このセクションは `guard check`/`internal/engine` が実際にどのようにコマンドを
ポリシーセットに対して解決するかを記述する —— ルールが期待通りに動かない
場合に役立つ。

1. **分解**: 生のBashコマンド行は実際のシェルASTパーサーでパースされ、
   すべての `CallExpr` を walk する —— `&&`、`;`、`|`、サブシェル、
   コマンド置換（`$(...)`）はすべて個別の `<tool> <args...>` 呼び出しに
   分解され、それぞれ独立して評価される。リテラルでない引数（変数展開など）
   は元のシェル構文のまま文字列化されるため、`functions` 一致の対象にはならない。
2. **コマンド単位のマッチング**: 分解された各コマンドについて、その
   `tool` を対象とするすべてのポリシーリソースがチェックされ、各リソース内で
   [`to` マッチング](#toマッチ条件)が最大1つのruleを選ぶ。
3. **CustomCheckによる上書き（存在する場合）**: マッチしたruleが
   `options.customCheck` を持つ場合、スクリプト/コマンドが実行され、その
   終了コードに対して `when` リストが評価される。マッチした場合は、
   そのリソース自身の `spec.action` を**常に上書き**する ——
   `DENY` や `CONFIRM` を素の `ALLOW` に格下げすることさえある。
   マッチしなかった場合（タイムアウトを含む）はリソース自身の `action`
   がそのまま残る。つまり: **`customCheck` > リソース自身の `action`**。
   他のリソースとの比較より前に、リソースごとに決まる。マッチした
   すべてのリソースがこの扱いになるため、別のリソースが最終結果を決める
   場合でも、そのリソースの `customCheck` は実行される。
4. **マッチしたリソース間でstrictest-wins**（安全策）: 同じコマンドにマッチし得る
   ポリシーは `guard lint` がエラーにする（[1コマンド1ポリシー](#1コマンド1ポリシー)）が、
   lintを無視したポリシー集合で複数マッチした場合は、その結果のうち最も厳しいactionが勝つ
   （`DENY > CONFIRM > ALLOW`）。最も厳しいactionで同率だったすべてのマッチする
   リソース/ruleのメッセージは重複を除いて結合される。
5. **何もマッチしない場合**: 分解されたコマンドにどのリソースのruleも
   マッチしない場合、組み込みのデフォルトが適用される —— action は
   `CONFIRM`、マッチしなかったコマンドを名指しした汎用的なreason、
   汎用的な「確認してください」というnext。このデフォルトはYAML上の
   `action` として書くことはできない。
6. **複合コマンドの集約**: 手順1-5は分解された各コマンドごとに1つの
   `Decision` を生成する。コマンド行全体の最終的な判定は、再度それら全体に
   対するstrictest-wins（手順4と同じ優先順位）で決まる。

## `guard lint` のルール

上記の表で説明したフィールド単位の制約（必須/固定語彙、path/commandの
排他チェック、`when[].key`のチェックなど）に加えて、`guard lint` は
以下もチェックする:

| 深刻度 | チェック内容 |
|---|---|
| エラー | `apiVersion` が `"ai-guard/v1"` と異なる |
| エラー | `kind` が `"CommandGatePolicy"` と異なる |
| エラー | `metadata.name` が空 |
| エラー | `spec.target.module` が `"permission"` と異なる |
| エラー | `spec.target.tool` が空 |
| エラー | `spec.action` が `ALLOW`/`DENY`/`CONFIRM` のいずれでもない |
| エラー | `spec.rules` が空 |
| エラー | `rules[].to.args` が設定されているが `anyOf` も `noneOf` も無い、またはどちらかが空文字列を含む |
| エラー | 1つのruleの `anyOf` と `noneOf` に同じトークンがある —— そのruleは絶対にマッチしない |
| エラー | 同じ tool の2つのポリシーが、あるコマンドに両方マッチし得る（[1コマンド1ポリシー](#1コマンド1ポリシー)を参照） |
| エラー | `rules[].options.customCheck` に `path` と `command` のどちらも無い、または両方ある |
| エラー | `rules[].options.customCheck.when[].key` が `"exit"` と異なる |
| 警告 | 同一リソース内に2つ目のcatch-all（`functions`省略）ruleがある —— 最初のものが既にすべてにマッチするため到達不可能 |

### 1コマンド1ポリシー

どのコマンドも、判定するポリシーはちょうど1つでなければならない。`guard lint` は
同じ `tool` のポリシーを2つずつ比べ、あるコマンド（function と args を合わせたもの）が
両方のruleにマッチし得る場合にエラーにする。`to.label` はマッチングに関与しないので
ポリシーを分ける役には立たず、1つのポリシー内のrule同士は比較されない（優先順位が決まっているため）。

1つのツールを複数のポリシーに分けるときは、`args.noneOf` で条件が重ならないようにする。
たとえば `git push` は CONFIRM、`git push -f` は DENY にする場合：

- `git/write-confirm.yaml`: `functions: [push]` に `args.noneOf: ["-f", "--force"]`
- `git/push-force-deny.yaml`: `functions: [push]` に `args.anyOf: ["-f", "--force"]`

## 例

### 最小のCONFIRM例

```yaml
# .guard/policies/curl/confirm.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: curl-confirm
spec:
  target:
    module: permission
    tool: curl
  action: CONFIRM
  rules:
    - to:
        label: network
      message:
        reason: "{cmd} accesses an external network resource."
        next: "Confirm the URL is safe with the user before proceeding."
```

### フラグ単位のマッチ（`noneOf` で重ならないようにする）

```yaml
# .guard/policies/git/push-force-deny.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-push-force-deny
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

`push` のruleに `args.noneOf: ["-f", "--force"]` を付けた `git/write-confirm.yaml`
（CONFIRM）と組み合わせることで、両者が同じコマンドにマッチしないようにする。
`git push` 単体は `CONFIRM`、`git push -f`/`--force` は `DENY` になる。

### CONFIRMフォールバック付きのCustomCheck

```yaml
# .guard/policies/git/push-custom-check.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-push-custom-check
spec:
  target:
    module: permission
    tool: git
  action: DENY             # 安全側のデフォルト: スクリプトが安全と示さない限り拒否
  rules:
    - to:
        label: write
        functions: [push]
      options:
        customCheck:
          path: "./scripts/check-push.sh"
          args: ["--staged"]
          timeoutSeconds: 10
          when:
            - key: exit
              value: 0
              action: CONFIRM   # 安全と判定されても一応人間に確認してもらう
          # when のどれにも一致しない場合（exit!=0やタイムアウト等）は spec.action=DENY にフォールバック
      message:
        reason: "{cmd} requires the push-safety script to pass before it runs."
        next: "Review the script's output with the user before proceeding."
```

### 複数ファイルの例

1つのツールに2つのaction、2つのファイルに分ける例:

```yaml
# .guard/policies/git/allow.yaml
spec:
  action: ALLOW
  rules:
    - to: { label: read, functions: [log, status, diff] }
```

```yaml
# .guard/policies/git/write-confirm.yaml
spec:
  action: CONFIRM
  rules:
    - to: { label: write, functions: [add, commit, push] }
```

```yaml
# .guard/policies/git/destructive-deny.yaml
spec:
  action: DENY
  rules:
    - to: { label: destructive, functions: [reset, clean, rebase] }
      message:
        reason: "{cmd} is a destructive operation and is denied."
        next: "Suggest an alternative approach to the user."
```
