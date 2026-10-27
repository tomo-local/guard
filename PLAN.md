# `guard report` 実装計画

`tmp/remaining-plan.md`に記載されている、唯一未実装のコマンド`guard report`の
実装計画。`.guard/logs/check.jsonl`と`.guard/logs/record.jsonl`を集計し、
テキスト（デフォルト）またはオフラインHTML（`--format html --out`）で出力する。

## 0. 前提として確認済みの事実

- `check.jsonl`の1行 = `sidecar.CheckLogEntry`（`internal/sidecar/check.go`）。
  `Mode`(hook/cli)、`Action`、`MatchedPolicies`、`Default`(フォールバック)、
  `UsedFallbackParse`、`CustomCheck`(ExitCode/TimedOut/Override)、
  `Commands[]`(`Tool`/`Action`/`Label`)を持つ。
- `record.jsonl`の1行 = `sidecar.RecordLogEntry`（`internal/sidecar/record.go`）。
  `SessionID`、`ToolUseID`、`Command`、`Interrupted`、`Stdout`/`Stderr`を持つ。
- **check側とrecord側を紐付けるIDは無い**：`PreToolUseInput`に`tool_use_id`は
  送られてこない（`PostToolUseInput`のみ）ため、`check.jsonl`に`ToolUseID`は
  記録されていない。→ 2つのログは厳密にJOINせず、それぞれ独立に集計する
  （record側は「実行された/中断された」の実績集計に留める）。
- **`tool+function`別の集計は現状のログでは不可能**：`CommandLog`には`Tool`と
  `Label`（自由記述・マッチには関与しない）しかなく、`functions`マッチに使われた
  実際のsubcommand文字列（`internal/engine/evaluate.go`の`firstFunction(args)`、
  `matchRule`内でのみ使用され外部に出ていない）が記録されていない。
  → 本計画で`CommandLog`に`Function`フィールドを追加し、`firstFunction`を
  `engine.Decision`経由で露出させる（下記 §2）。**この変更より前に書かれた
  既存の`check.jsonl`には`function`が無い**ので、report側は`function`欠落を
  前提に後方互換で扱う（空文字は「不明」として集計からは除外、エラーにはしない）。

## 1. スコープ（v1）

- 入力: `.guard/logs/check.jsonl` + `.guard/logs/record.jsonl`（両方読めなくても
  片方だけで動く。両方無ければ「ログがありません」と案内して終了、exit 0）。
- フィルタなし（日付範囲やtool指定での絞り込みは今回は実装しない。
  `guard-reports`スキルの記述通り、聞かれたら「まだ未対応」と答える）。
- 出力はテキスト（デフォルト、stdout）とHTML（`--format html --out <path>`）の2種。
- 既存コマンド（check/record/lint/init）の挙動・ログ形式には影響を与えない
  （`CommandLog.Function`追加は追記のみで既存フィールドは変更しない）。

## 2. 既存コードへの小変更（`guard report`の前提）

### `internal/engine/evaluate.go`

- `Decision`に`Function string`を追加（`EvaluateCommand`が`Tool`と同様に設定）。
- `firstFunction(args)`の呼び出し結果を`matchRule`の戻り値経由ではなく、
  `EvaluateCommand`内で直接計算して`Decision.Function`に詰める
  （`matchRule`は現状`function`をローカル変数としてしか使っていないため、
  `EvaluateCommand`側でもう一度`firstFunction(cmd.Args)`を呼ぶだけで済む。
  二重計算のコスト上の問題は無い）。

### `internal/sidecar/check.go`

- `CommandLog`に`Function string `json:"function,omitempty"`` を追加。

### `cmd/check.go`

- `buildLogEntry`の`Commands`構築ループで`Function: d.Function`を追加。

### テスト更新

- `internal/engine/evaluate_test.go`: 既存テストに`Function`の期待値を追加
  （既存アサーションを壊さない範囲で）。
- `internal/sidecar`側に`CommandLog`の新フィールドを検証するテストは不要
  （構造体フィールド追加のみ、ロジックなし）。

## 3. `guard report`本体

### 3.1 CLIインターフェース（`cmd/report.go`、薄いグルーのみ）

```
Usage:
  guard report [-rules <dir>] [-log <path>] [-record-log <path>] [-format text|html] [-out <path>]

Options:
  -log string         check decision log path (default ".guard/logs/check.jsonl", env GUARD_LOG_PATH)
  -record-log string  record log path (default ".guard/logs/record.jsonl", env GUARD_RECORD_LOG_PATH)
  -format string       "text" (default, stdout) or "html"
  -out string           output path, required when -format html; ignored for text (always stdout)

Exit codes: 0=success (even when logs are empty/missing -- a thin report is
not an error), 1=usage error (e.g. -format html without -out).
```

既存コマンドと同じ`-flag > env var > default`規約に合わせ、`-log`/`-record-log`は
既存の`defaultLogPath()`/`defaultRecordLogPath()`（`cmd/env.go`）を再利用する。
`-rules`は今回のv1集計には使わない（ポリシー定義そのものの参照は不要）ため、
**フラグとしては追加しない**（スコープを絞る。将来「現在のポリシーに存在しない
matched_policiesパス」を検出する等の用途が出たら追加検討）。

### 3.2 `internal/report`パッケージ（ロジック本体、cmdにテストを増やさない方針を継続）

```
internal/report/
├── analyze.go       # JSONLを読んで集計結果（Report構造体）を作る
├── analyze_test.go
├── render_text.go   # Reportをテキストに整形
├── render_html.go   # Reportをgo:embedテンプレートでHTMLに整形
├── render_test.go
└── template.html    # go:embed対象、外部CDN無し・インラインCSSのみ
```

#### `analyze.go`

```go
package report

type Report struct {
    GeneratedAt   time.Time
    CheckEntries  int      // 読み込めた check.jsonl の行数
    RecordEntries int      // 読み込めた record.jsonl の行数
    CheckLogErr   error    // ログが無い/壊れている場合も集計は続行し、ここに記録
    RecordLogErr  error

    ActionCounts     map[string]int // "ALLOW"/"DENY"/"CONFIRM" -> count（最終決定ベース）
    ModeCounts       map[string]int // "cli"/"hook" -> count
    DefaultCount     int            // Default==true の件数（ポリシー未マッチでフォールバックした回数）
    FallbackParseCount int          // UsedFallbackParse==true の件数

    ToolActionCounts map[string]map[string]int // tool -> action -> count（CommandLogベース、Toolが空のdefault決定は除外）
    TopDenyConfirm   []ToolFunctionCount        // tool+function別、DENY+CONFIRM件数の降順トップN（function空は除外）

    CustomCheckRuns     int
    CustomCheckTimeouts int
    CustomCheckOverrides map[string]int // override action -> count（override==""は除外）

    DailyTimeline []DailyCount // 日付(UTC, YYYY-MM-DD)昇順、check件数のaction別ブレークダウン

    RecordInterrupted int // record.jsonl 側、Interrupted==true の件数
}

type ToolFunctionCount struct {
    Tool, Function string
    DenyCount, ConfirmCount int
}

type DailyCount struct {
    Date   string // YYYY-MM-DD
    Counts map[string]int // action -> count
}

// Analyze reads checkLogPath/recordLogPath (either may not exist) and
// builds a Report. It never returns an error itself -- a missing or
// unparseable log is recorded in Report.CheckLogErr/RecordLogErr and
// skipped, since a report should degrade gracefully rather than block.
func Analyze(checkLogPath, recordLogPath string) Report
```

- JSONL読み込みは1行ずつ`bufio.Scanner`＋`json.Unmarshal`。**壊れた行（JSON
  パース失敗）はスキップしてカウントのみ増やす**（`SkippedLines`としてReportに
  追加）。1行のエラーで全体を諦めない。
- トップN（`TopDenyConfirm`）のNは**5固定**（v1ではフラグ化しない。既存の
  `options.customCheck.timeoutSeconds`のように後で必要になったら足す方針に揃える）。
- 集計は`Action`文字列をそのまま`map`のキーに使う（`policy.Action`型に依存せず
  `internal/report`が`internal/policy`に依存しなくて済むようにする——
  reportはログの集計であって、ポリシー評価ロジックには関与しない層なので、
  既存の層分離（engine/policy/sidecar/report）を保つ）。

#### `render_text.go`

- セクション見出し＋固定幅の簡易テーブルをstdout相当の`io.Writer`に書く
  （既存`cmd/lint.go`等と同じ、外部テーブルライブラリは使わない——
  `craft-common.md`ではないが、このCLI全体で標準ライブラリのみという
  README記載の方針に合わせる）。
- 出力構成（上から順）: 概要（件数・期間）→ Action別集計 → Mode別集計 →
  Default/FallbackParse発生率 → Tool別Action集計 → DENY/CONFIRM上位
  tool+function → customCheck集計 → 日次タイムライン → record側の実行実績。

#### `render_html.go` + `template.html`

- `go:embed template.html`し、`html/template`（**`text/template`ではなく
  `html/template`**——ログ内容（コマンド文字列等）を埋め込むため、エスケープ
  必須。コマンド文字列をHTMLに出す箇所があるならXSS類のリスクがあるため、
  ユーザー入力起源のデータをテンプレートに渡す以上ここは徹底する）。
- CSSはインライン`<style>`、JSは使わない（固定の静的レポート。README/
  このプロジェクトの「外部依存を増やさない」方針に合わせ、Chart.js等は使わない。
  棒グラフ等はCSSの`width:%`やインラインSVGで十分表現できる範囲に留める）。
- 外部CDN・フォント読み込み無し（オフライン前提、`tmp/remaining-plan.md`の
  要求どおり）。

### 3.3 `cmd/report.go`（グルー）

```go
func (c *Cmd) RunReport(args []string) error {
    // flag定義（-log, -record-log, -format, -out）
    // report.Analyze(checkLogPath, recordLogPath)
    // format=="html" なら report.RenderHTML(w, rpt) を -out に書き出し、
    //   stdoutには「written to <path>」の1行だけ出す
    // それ以外（デフォルト）は report.RenderText(c.Stdout, rpt)
}
```

- `main.go`の`switch command`に`case "report": err = c.RunReport(args)`を追加し、
  `showHelp()`のコマンド一覧にも`report`を追記。

## 4. テスト計画

- `internal/report/analyze_test.go`:
  - 空ログ（ファイル無し）→ 全カウント0、`CheckLogErr`/`RecordLogErr`が
    「ファイル無し」を示す（が全体はpanicもエラー終了もしない）。
  - 正常系のcheck.jsonl（ALLOW/DENY/CONFIRM混在、hook/cli混在、customCheck
    あり/なし、Default発生あり）→ 各集計フィールドが期待通り。
  - 壊れた行を1行混ぜても残りは集計される。
  - `TopDenyConfirm`が件数降順で、同数時は決定的な順序（tool名→function名の
    辞書順などタイブレークを明記）になる。
- `internal/report/render_test.go`:
  - `RenderText`: 固定のReportを渡し、キーワード（action名、件数）が出力に
    含まれることを確認（完全一致のゴールデンテキストは保守コストが高いので、
    部分一致/構造チェックに留める）。
  - `RenderHTML`: 出力が妥当なHTML（`html.Parse`等で構文エラーが無いこと）で、
    コマンド文字列に`<script>`等を混ぜたケースでエスケープされていることを
    確認（XSS回避の確認）。
- `internal/engine/evaluate_test.go`: `Decision.Function`の期待値を既存テストに追加。

## 5. 検証手順（実装後）

1. `go build ./... && go vet ./... && go test ./...`
2. `guard report`（ログが空の状態）→ 「ログがありません」系の案内がエラー
   終了せず出ることを確認。
3. `.guard/logs/`にサンプルデータを投入（`guard check`/`guard record`を
   複数パターンで実行）→ `guard report`のテキスト出力を目視確認。
4. `guard report --format html --out /tmp/guard-report.html`→ ブラウザで
   開いて崩れていないこと、外部リクエストが発生しないこと（オフラインでも
   表示できること）を確認。
5. `.claude/skills/guard-reports/SKILL.md`を実際に`/guard-reports`として
   動かし、ステータスチェックの分岐（実装済みなので「未実装」フォールバックに
   入らず、本来の集計リレーの手順1〜4に進むこと）を確認。
6. README「Commands」セクションに`guard report`の節を追加し、「`guard report`
   is the only piece still pending」の記述を削除する。`docs/ja/README.md`も追従。
7. `tmp/remaining-plan.md`を削除する（計画メモ自体の完了後片付け、と
   そのファイル自身に書かれている通り）。

## 6. 今回やらないこと（将来検討）

- 日付範囲・tool指定でのフィルタリング（`guard-reports`スキルも「未対応」と
  案内する前提で書かれている）。
- `check.jsonl`と`record.jsonl`の厳密なJOIN（ID不足のため）。必要になったら
  `PreToolUseInput`側にID相当が無い制約を再確認し、`session_id`+コマンド文字列
  +タイムスタンプ近傍マッチのような近似JOINを別途検討。
- `tmp/record-post-api-plan.md`（外部HTTP POST対応）とは独立の作業であり、
  本計画のスコープ外。
