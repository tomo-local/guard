# GuardManifest スキーマリファレンス

`.guard/manifest.yaml` はプロジェクト全体の大枠のルールを書くファイル（`kind: GuardManifest`）。
ツールごとの[ポリシー](policy-schema.md)の**後ろ**に置かれる：コマンドはまず `.guard/policies/**`
に照らされ、どのポリシーも意見を持たなかった場合にだけ manifest が判定する。

デフォルトでは `.guard/manifest.yaml` から読み込む（`-manifest` / `GUARD_MANIFEST_PATH`）。
ファイルが無くても問題ない（manifest が無いだけ）。manifest は1つだけ置ける。

## 構造

```yaml
apiVersion: ai-guard/v1        # 必須、ちょうど "ai-guard/v1"
kind: GuardManifest            # 必須、ちょうど "GuardManifest"
metadata:
  name: <string>               # 必須
spec:
  commandGate:                 # 任意：コマンドが触るパスで制限する
    target:
      action: DENY             # 任意：`action` を書かない rule の既定。省略時は DENY
    rules:                     # 1件以上
      - to:
          label: <string>      # 任意、説明用
          paths:               # 必須
            anyOf: ["/cmd/*"]  # 空でない glob パターンを1件以上
        action: CONFIRM        # 任意：ALLOW | DENY | CONFIRM（target.action を上書き）
        message:               # 任意、英語、使えるプレースホルダは `{cmd}` のみ
          reason: "..."
          next: "..."
  repositoryGate: {...}        # 読み込むが未対応（lint が warning）
  sqlGate: {...}               # 読み込むが未対応（lint が warning）
```

## `commandGate` の判定方法

1. コマンドの引数のうち `-` で始まらないものはすべてパス候補になる。相対パスは `guard` を実行した
   ディレクトリ基準で絶対化し、**プロジェクトルート**（`.guard/` があるディレクトリ）からの相対に直す。
   プロジェクト外のパスとルート自身は無視される。
2. `paths.anyOf` のいずれかのパターンがいずれかの候補にマッチすれば、その rule がマッチ。
3. 複数の rule がマッチした場合は、最も厳しい action が勝つ（`DENY > CONFIRM > ALLOW`、同率なら先に書いた方）。
   判定には、その rule の `message`（無ければパスを示す汎用メッセージ）が使われる。
4. 複合コマンド（`a && b`）は先に分解され、単純コマンドごとに評価し、他と同じく最も厳しい結果が勝つ。

### パターン

パターンはプロジェクトルートを起点とし、先頭の `/` は省略できる。

| パターン | マッチ |
|---|---|
| `/cmd/*` | `cmd/a.go`（`cmd/x/a.go` にはマッチしない） |
| `/cmd/**` | `cmd/` の下のすべて（深さは問わない） |
| `/**/*.go` | 任意の深さの `.go` ファイル |
| `/src/[ab]/*` | `[...]` と `?` は `path.Match` に従う |

`*` は1つのパスセグメントの中だけ、セグメント全体が `**` の場合は任意個のセグメントにまたがる。
パターンのリテラルな先頭部分より上のディレクトリも対象になるので、`/cmd/*` は `cmd` 自身にもマッチする
（`rm -rf cmd` を検知できる）。先頭がワイルドカードのパターン（`/**/*.go`）は、上位ディレクトリを対象にしない。

### 既知の制限

- コマンド内での移動は追跡しない：`cd cmd && rm a.go` の `a.go` は `cmd/` ではなく、`guard` を実行した
  ディレクトリ基準で解決される。
- パスに見えるものは何でも候補になる（クォート内の文字列も含む。`git commit -m "/cmd/x"` は触るパスでは
  ないが、パスとして扱われる）。manifest はどのポリシーもマッチしなかったコマンドだけを判定するので、
  誤検知した場合は rule の action（多くは CONFIRM か DENY）になる。
- `guard` を実行したディレクトリはプロセスから取り、hook payload の値は使わない。

## `guard lint`

| 重大度 | チェック |
|---|---|
| エラー | `apiVersion`/`kind`/`metadata.name` が無い、または不正 |
| エラー | `commandGate.rules` が空、`to.paths` の無い rule、`paths.anyOf` が空または空文字を含む |
| エラー | `target.action` または rule の `action` が `ALLOW`/`DENY`/`CONFIRM` のいずれでもない |
| エラー | `paths.anyOf` に不正なパターンがある |
| エラー | `GuardManifest` が2つ以上ある |
| 警告 | `repositoryGate` / `sqlGate` に中身がある（未対応） |

## 例

```yaml
# .guard/manifest.yaml
apiVersion: ai-guard/v1
kind: GuardManifest
metadata:
  name: default-rules
spec:
  commandGate:
    target:
      action: DENY
    rules:
      - to:
          label: cmd-dir
          paths:
            anyOf: ["/cmd/*"]
        message:
          reason: "No policy matched {cmd}, which touches /cmd; denying by default."
          next: "Add an explicit allow/deny rule under .guard/policies/ for this command."
      - to:
          label: src-dir
          paths:
            anyOf: ["/src/*"]
        action: CONFIRM
        message:
          reason: "{cmd} is a write operation under /src."
          next: "Confirm with the user before proceeding."
```

このリポジトリのポリシーでは、`cat cmd/a.go` は `DENY`、`touch src/a.go` は `CONFIRM`、
`git status` は `ALLOW` のまま（ポリシーがマッチしたので manifest は参照されない）、
`ls .` は通常のデフォルト `CONFIRM`。
