# データベース関連ファイルのリファレンス

データベースクライアント（`psql`、`mysql`）は、`.guard/db/` の2つのファイルで制御する
（ディレクトリは `GUARD_DB_DIR` / `-db` で変更できる）。どちらも任意。

- `connections.yml`（`kind: GuardDBConnections`）は、接続先のホストとデータベースから**環境**
  （production、staging など）を定義する。判定はしない。
- `policy.yml`（`kind: GuardDBPolicy`）は、開く接続の環境に応じて、コマンドをどうするかを決める。

コマンドは、まず `.guard/policies/` のツールごとのポリシーに、次に `.guard/db/policy.yml` に、最後に
プロジェクトの manifest に照らされる。`.yml` でも `.yaml` でも読める。manifest の `sqlGate`
（SQL 文そのものの判定用に予約。未実装）とは別のもの。

## `connections.yml`

```yaml
apiVersion: ai-guard/v1
kind: GuardDBConnections
metadata:
  name: connections
spec:
  environments:
    - name: production
      hosts: ["*.prod.example.com", "10.0.0.5"]
    - name: reporting
      hosts: ["*.example.com"]
      databases: ["report_*"]
    - name: local
      hosts: [localhost, 127.0.0.1]
```

| フィールド | 必須 | 備考 |
|---|---|---|
| `environments[].name` | はい | 重複不可。 |
| `environments[].hosts` | `hosts`/`databases` のどちらか | `path.Match` パターン。大文字小文字は区別しない。 |
| `environments[].databases` | `hosts`/`databases` のどちらか | 同上。データベース名のない接続は、`databases` を持つ環境にはマッチしない。 |

接続がある環境に属するのは、`hosts`（あれば）**と** `databases`（あれば）の両方にマッチするとき。
複数の環境に属してもよい。ホストを指定しないコマンドは `localhost` に接続するものとして扱う。

## `policy.yml`

```yaml
apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata:
  name: db-policy
spec:
  target:
    clients: [psql, mysql]   # 任意。省略時は両方
    action: CONFIRM          # 任意。`action` を書かない rule の既定。省略時は DENY
  rules:
    - to:
        label: production    # 任意、説明用
        environment:
          anyOf: [production]
      action: DENY           # 任意
      message:               # 任意、英語、使えるプレースホルダは `{cmd}` のみ
        reason: "{cmd} connects to a production database."
        next: "Ask the user to run it themselves."
    - to:
        environment:
          noneOf: [production, staging, local]   # どの既知の環境にも属さない接続
```

`to.environment` は `anyOf`（接続が少なくとも1つの環境に属する）と `noneOf`（どれにも属さない）を取る。
[`to.branch`](policy-schema.md#toマッチ条件) と同じ形。名前はすべて `connections.yml` に定義されている必要がある。
複数の rule がマッチした場合は最も厳しい action が勝つ（`DENY > CONFIRM > ALLOW`、同率なら先に書いた方）。
`connections.yml` が無ければ、どの rule もマッチしない。

## 接続先の読み取り元

コマンドラインだけ：

- `psql`：`-h`/`--host`、`-d`/`--dbname`、位置引数のデータベース名、`postgres://` または `postgresql://` の URI
  （`-d` でも位置引数でも）、`"host=db.example.com dbname=app"` のような接続文字列。`-h` は URI の中のホストより優先される。
- `mysql`：`-h`/`--host`、`-D`/`--database`、位置引数のデータベース名、`mysql://` の URI。`-p` に付けたパスワードは無視する。

他のフラグの値（`-U`、psql の `-p`、`-c`、`-e` など）をデータベース名と取り違えることはない。

### 既知の制限

- 環境変数（`PGHOST`、`DATABASE_URL` など）、`~/.pgpass`、サービスファイル、スクリプトファイル（`-f`）や標準入力は
  読まないので、それらに依存する接続は `localhost` に見える。
- 対象は `psql` と `mysql` のみ。
- SQL 文（`DROP`、`DELETE` など）の判定は未実装。

## `guard lint`

| 重大度 | チェック |
|---|---|
| エラー | `connections.yml`：`apiVersion`/`kind`/`metadata.name` が不正、環境が無い、名前が重複または空、`hosts` も `databases` も無い環境、不正なパターン、接続ファイルが2つ以上 |
| エラー | `policy.yml`：`apiVersion`/`kind` が不正、rule が無い、`to.environment` の無い rule、未知のクライアント、`ALLOW`/`DENY`/`CONFIRM` 以外の `action`、`anyOf`/`noneOf` が両方空または同じ名前を共有、DB ポリシーのファイルが2つ以上 |
| エラー | rule が `connections.yml` に定義されていない環境を参照している（または接続ファイルが無い） |
