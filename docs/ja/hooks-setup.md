# `guard` を Claude Code の hook に組み込む

`guard` は Claude Code の hook として自動で登録はされない。`guard init` は下記の
スニペットを標準出力に表示するだけで、`.claude/settings.json` を自動で編集することは
ない（既存の hook 設定を壊してしまうリスクを避けるため）。

## 追加する内容

プロジェクトの `.claude/settings.json` に以下を追加する（ファイルが無ければ新規作成）:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "guard check" }]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "guard record" }]
      }
    ]
  }
}
```

既に `hooks` キーがある場合は、既存の `PreToolUse`/`PostToolUse` 配列を置き換えずに
これらのエントリを追記（マージ）すること。

- **`PreToolUse` → `guard check`**: Claude Code がBashコマンドを実行する**直前**に
  毎回走る。`.guard/policies/` と照合し、`hookSpecificOutput.permissionDecision` で
  `allow`/`deny`/`ask` を返す。実際にコマンドをゲートしているのはこれで、
  このエントリが無いと `guard` は何の効果も持たない。
- **`PostToolUse` → `guard record`**: Bashコマンドの実行**後**に走り、実際に何が
  起きたか（コマンド・stdout/stderrの生出力・中断されたか）を
  `.guard/logs/record.jsonl` に追記する。何もブロックせず常に exit 0。
  `guard report` による事後分析のためだけに使う。

## コマンドの解決方法

上記スニペットはどちらも `guard` が `PATH` 上で解決できる前提
（Homebrewでインストール済み、または `go install` 済みなど）。ソースから
チェックアウトしてインストールしていない場合は、ビルド済みバイナリへの
パスを直接指定する:

```json
{ "type": "command", "command": "/absolute/path/to/ai-guard-tracker/bin/guard check" }
```

（事前に `go build -o bin/guard .` でビルドしておく）。

## ポリシー/ログの出力先をカスタムにする場合

`.guard/policies/` やログの出力先をデフォルトと異なる場所に置いているプロジェクトでは、
hookコマンド自体に `-rules`/`-log` フラグを埋め込むより、Claude Code の hook が実行
される環境に `GUARD_POLICIES_DIR` / `GUARD_LOG_PATH` / `GUARD_RECORD_LOG_PATH` を
設定する方がよい（README の環境変数の表を参照）。hookコマンドに明示的に
`-flag` を渡した場合はそちらが環境変数より優先される。

## 正しく設定できたか確認する

```bash
guard check "git reset --hard"
```

を実行すると `DENY ...` が表示され、exit codeが非0になるはず。hookが正しく設定
されていれば、Claude Code に同じコマンドをこのプロジェクトで実行させようとした際、
実際にシェルに届く前にブロック（またはポリシー次第で確認待ち）になる。
