# ADR-0102: 保管の Bucket Policy は、主体の例外を持たない Deny と、型付きの書き手のサービスへの Allow だけを持つ

- Status: Accepted
- Date: 2026-11-19
- Scope: audit-evidence
- Related: repository-wide/ADR-0204, repository-wide/ADR-0301, audit-evidence/ADR-0101, audit-evidence/ADR-0201

## Context

証跡を書くのは AWS のサービス（組織の trail と AWS Config）だけである。人と CI の主体は object を書かず、消さず、保持を変えない。Object Lock は版の削除を防ぐが、delete marker を置く削除、既定より長い保持の指定、legal hold と event hold による保持の延長は防がない（Amazon S3 User Guide「Object Lock considerations」）。

サービスへの Allow に要求元の条件が無いと、別のアカウントの trail や Config から書き込める（confused deputy）。組織の trail の `aws:SourceArn` は管理アカウントが所有する trail の ARN である（CloudTrail User Guide「Amazon S3 bucket policy for CloudTrail」）。Bucket Policy は自分自身の書き換えと削除を防げない。

## Decision

1. Bucket Policy の Deny は Principal `"*"` で、主体による例外（`aws:PrincipalArn` など）を持たない: TLS でない要求、サービスでない主体の `s3:PutObject`、`s3:DeleteObject`・`s3:DeleteObjectVersion`、既定の保持の日数を超える保持の指定、既定の mode 以外の mode の指定、legal hold と event hold。
2. Allow はサービス principal にだけ置き、要求元を wildcard の無い値で限る。組織の trail は `aws:SourceArn` を trail の ARN の完全一致で、Config は `aws:SourceAccount` をアカウントの ID で限る。
3. 書き手は種類ごとの固定のキーを持つ型付きの入力 `writers` で受ける（`cloudtrail_organization_trail` は trail の名前・所有するアカウント・組織の ID・リージョン、`config_source_accounts` はアカウントの ID）。ARN と object の prefix は module が組む。新しい種類は、その書き手の要求を契約へ一般化するときに足す（repository-wide/ADR-0102 (use-case-centric-scope) 決定5）。
4. Config の書き込みの文はアカウントごとに分け、あるアカウントの Config が他のアカウントの prefix に書けないようにする。
5. SSE-KMS 以外の書き込みを拒む Deny を置かない。CloudTrail の log file validation の digest の配信が壊れる（CloudTrail User Guide「Security best practices」）。
6. Bucket・鍵・配送の設定を変える操作の拒否は、Bucket Policy ではなく、組織の SCP（`protect-log-vault`）が持つ。

## 検討した代替案

### 案A: CI の apply の role を Deny の例外に名指す

role の ARN を root をまたいで受ける口が増え、その role を奪われると証跡を消せる。Bucket Policy は自分の削除を防げないので、設定の変更を防ぐ効果も無い。

### 案B: 書き手の ARN や policy の文を入力で受ける

値の意味が AWS の表現に依存する Generic Escape Hatch になる（repository-wide/ADR-0204 (no-generic-escape-hatch) 決定2）。

### 案C: Config の書き込みを1文で全アカウントへ許す

あるアカウントの Config が、他のアカウントの prefix に偽の記録を書ける。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中（例外の role が消せる） | 低 | 中 | 高 |
| Testability | 高 | 低 | 高 | 高 |
| Contract Clarity | 中 | 低 | 高 | 高 |
| Maintainability | 中 | 低 | 高 | 高 |
| Scope Control | 中 | 低 | 高 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

証跡の訴訟のための保全（legal hold）と、書き手が object ごとに保持を指定すること。

## 保証範囲

- 保証すること: Bucket Policy の文書が、入力から決まる Deny の8文と書き手の Allow の集合に一致すること。Allow がサービス principal だけで、要求元を wildcard の無い値で限ること。Deny が主体の例外を持たないこと。Bucket Policy と鍵の Key Policy が名指す trail が一致すること
- 保証しないこと: `protect-log-vault` が入るまで、log-archive のアカウントで S3 の管理の権限を持つ主体が Bucket Policy を書き換えること・消すことを防ぐこと。Deny が実際の API で拒むこと

## 検証方法

- Unit Test: `internal/archive-bucket/tests/policy.tftest.hcl` が、Config の有無と mode ごとに文書の完全一致を mock の apply で確かめる
- Policy Test: `PLAN-AUDIT-02`（Allow の形）、`PLAN-AUDIT-03`（Deny の存在・例外の不在・日数と mode の一致）、`PLAN-AUDIT-05`（trail の一致）
- Deny が実 API で拒むことは sandbox の検証で確かめる。どのテストも見ない

## 影響

人の調査で object を読む経路は、Bucket Policy には無い。読み手は鍵の Key Policy に名指されるまで復号できない。

## 見直し条件

- GuardDuty の finding の書き出しなど、新しい書き手の種類を契約へ足すとき
- legal hold を要る要求が出たとき
- `protect-log-vault` が入ったとき（保証しないことの1つ目が外れる）
