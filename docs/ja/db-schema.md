# データベース関連ファイルのリファレンス

データベースクライアント（`psql`、`mysql`）は、`.guard/db/` の2つのファイル（ディレクトリは
`GUARD_DB_DIR` / `-db` で変更できる）と、manifest の `sqlGate` で制御する。どれも任意。

- `connections.yml`（`kind: GuardDBConnections`）は**環境**（production、staging など）と、環境が持つ
  **ラベル**を定義する。判定はしない。
- `policy.yml`（`kind: GuardDBPolicy`）は、ラベルごとの SQL のルールを書く：`prod` ラベルの接続に何を
  実行してよいか、など。
- manifest の [`sqlGate`](manifest-schema.md#sqlgate全環境に効くsqlのルール) は、環境に関係なく効く SQL のルール。

コマンドは、まず `.guard/policies/` のツールごとのポリシーに、次に `.guard/db/policy.yml` に、最後に
manifest（`sqlGate` と `commandGate`）に照らされる。`.yml` でも `.yaml` でも読める。

## `connections.yml`

```yaml
apiVersion: ai-guard/v1
kind: GuardDBConnections
metadata:
  name: connections
spec:
  environments:
    - name: production
      labels: [prod]
      cli:
        hosts: ["*.prod.example.com", "10.0.0.5"]
    - name: reporting
      labels: [reports]
      cli:
        hosts: ["*.example.com"]
        databases: ["report_*"]
    - name: local
      labels: [dev]
      cli:
        hosts: [localhost, 127.0.0.1]
```

| フィールド | 必須 | 備考 |
|---|---|---|
| `environments[].name` | はい | 重複不可。識別とメッセージ用で、ルールからは参照しない。 |
| `environments[].labels` | いいえ | `policy.yml` が参照するもの。環境内で重複不可。 |
| `environments[].cli` | 接続方法のどれか1つ | `psql` / `mysql` のコマンドラインをどう見分けるか。接続を見分ける項目が接続方法ごとに違うため、方法ごとにブロックを分ける（MCP クライアントなど、他の方法は専用のブロックを足す予定）。未知のキーはエラー。 |
| `cli.hosts` | `hosts`/`databases` のどちらか | `path.Match` パターン。大文字小文字は区別しない。 |
| `cli.databases` | `hosts`/`databases` のどちらか | 同上。データベース名のない接続は、`databases` を持つ環境にはマッチしない。 |

接続がある環境に属するのは、`cli.hosts`（あれば）**と** `cli.databases`（あれば）の両方にマッチするとき。
複数の環境に属してもよく、その場合はすべての環境のラベルを持つ。ホストを指定しないコマンドは `localhost` に接続するものとして扱う。

## `policy.yml`

ツールごとのポリシーと同じ書き方：`functions` がルールの対象を表す（ここではサブコマンドの代わりに
SQL 文のキーワード）。`functions` の無いルールは catch-all。

```yaml
apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata:
  name: db-policy
spec:
  target:
    clients: [psql, mysql]   # 任意。省略時は両方
    action: DENY             # 任意。`action` を書かない rule の既定。省略時は DENY
  rules:
    - to:
        label: prod-schema   # 任意、説明用
        labels: [prod]
        functions: [DROP, TRUNCATE, ALTER]
      action: DENY
      message:               # 任意、英語、使えるプレースホルダは `{cmd}` のみ
        reason: "{cmd} changes the schema of a production database."
        next: "Ask the user to run it themselves."
    - to:
        labels: [prod]
        functions: [SELECT, SHOW]
      action: ALLOW
    - to:
        labels: [prod]       # functions 無し：prod のその他すべての文
      action: CONFIRM
```

| フィールド | 備考 |
|---|---|
| `to.labels` | 環境がこのラベルを**少なくとも1つ**持つ接続に適用する。省略すると、どの接続にも適用する。すべてのラベルが `connections.yml` に定義されている必要がある。 |
| `to.functions` | SQL 文のキーワード（大文字小文字は区別しない）。省略すると catch-all。 |
| `action` | `ALLOW` / `DENY` / `CONFIRM`。省略時は `target.action`、それも無ければ `DENY`。 |

### rule の選ばれ方

コマンドの SQL の各文を、それぞれ独立に判定する：

1. `labels` が接続に合う rule だけが候補（`labels` の無い rule はどの接続にも合う。`connections.yml` が無いと、
   ラベルを書いた rule は合わない）。
2. その中で、`functions` に文のキーワードを含む最初の rule（ファイルの順）が勝つ。
3. 無ければ、最初の catch-all（`functions` なし）が適用される。
4. どの rule も無ければ、このポリシーは意見なしで、manifest の `sqlGate` が判定する。

コマンドが複数の文を実行する場合は、最も厳しい結果が採用される（`DENY > CONFIRM > ALLOW`）：
`select 1; drop table x` は `DROP` の rule で判定される。

## 接続先と SQL の読み取り元

コマンドラインだけ。

- 接続先、`psql`：`-h`/`--host`、`-d`/`--dbname`、位置引数のデータベース名、`postgres://` または
  `postgresql://` の URI（`-d` でも位置引数でも）、`"host=db.example.com dbname=app"` のような接続文字列。
  `-h` は URI の中のホストより優先される。`mysql`：`-h`/`--host`、`-D`/`--database`、位置引数のデータベース名、
  `mysql://` の URI。
- SQL、`psql`：すべての `-c`/`--command`。`mysql`：すべての `-e`/`--execute`。
- 値を囲むシェルのクォートは先に外す：`-c 'drop table x'` は `drop table x` として読む。

### 文とキーワード

SQL テキストは `;` で分割する（引用・コメント・`$$` ブロックの中は分割しない）。文の**キーワード**は最初の語を
大文字にしたもの（`select` → `SELECT`）。例外：

| 文 | キーワード |
|---|---|
| `WITH ...` | 中にある `DELETE` > `UPDATE` > `MERGE` > `INSERT` のうち一番厳しいもの。無ければ `SELECT` |
| `EXPLAIN ANALYZE <文>` | その文のキーワード（実行されるため） |
| `EXPLAIN <文>` | `EXPLAIN` |
| psql のメタコマンド（`\copy` など） | バックスラッシュ付きの語（`\COPY`） |
| コマンドラインに SQL が無い（`-f file`、標準入力、対話、空の `-c`） | `UNKNOWN` |

### 既知の制限

- スクリプトファイル（`-f`）、標準入力、対話セッションは読まない：`UNKNOWN` になるので、
  `functions: [UNKNOWN]` の rule でそれらをどうするか決める。
- 見るのは最初の語（と上記の `WITH` / `EXPLAIN`）だけ。サブクエリ、関数呼び出し、`SELECT ... INTO` は解析しない。
- 環境変数（`PGHOST`、`DATABASE_URL` など）、`~/.pgpass`、サービスファイルは読まないので、それらに依存する接続は
  `localhost` に見える。
- 対象は `psql` と `mysql` のみ。

## `guard lint`

| 重大度 | チェック |
|---|---|
| エラー | `connections.yml`：`apiVersion`/`kind`/`metadata.name` が不正、環境が無い、名前が重複または空、接続方法が無い環境、`hosts` も `databases` も無い `cli`、不正なパターン、空または重複したラベル、未知のキー、接続ファイルが2つ以上 |
| エラー | `policy.yml`：`apiVersion`/`kind` が不正、rule が無い、未知のクライアント、`ALLOW`/`DENY`/`CONFIRM` 以外の `action`、空のラベルや function、DB ポリシーのファイルが2つ以上 |
| エラー | rule が、どの環境も持っていないラベルを参照している（または接続ファイルが無い） |
| 警告 | すべての接続のすべての文に効く rule（`labels` も `functions` も無い）が2つ目にある：最初のものがすべてに効くため到達不可能 |
