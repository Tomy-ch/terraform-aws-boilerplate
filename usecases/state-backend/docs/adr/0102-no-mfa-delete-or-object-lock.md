# ADR-0102: state Bucket に MFA delete と Object Lock を掛けない

- Status: Accepted
- Date: 2026-10-25
- Scope: state-backend
- Related: repository-wide/ADR-0207, repository-wide/ADR-0604, repository-wide/ADR-0602

## Context

S3 は、版の削除と版管理の停止に MFA を要求する MFA delete と、object の上書きと削除を期間で禁じる Object Lock を持つ。Security Hub の S3.20 は MFA delete を推奨する。

MFA delete は Bucket の所有者（root）しか有効にできず、lifecycle の設定と併用できない。bootstrap は root を使わず（`docs/setup/bootstrap.md`）、段5 で lifecycle を入れる。Object Lock は、S3 backend が lock file（`<key>.tflock`）を置いて消す動作と、sandbox の複製の破棄に干渉する。

## Decision

1. state Bucket に MFA delete を有効にしない。
2. state Bucket に Object Lock を有効にしない。
3. state の上書きと削除への耐性は、版管理（`Enabled`）と、bootstrap の段5 で入る拒否（state と lock file の書き込みを apply の role ごとに限る。repository-wide/ADR-0604 (state-split-by-apply-role) 決定9・10）で担う。
4. 決定1・2 を公式推奨からの逸脱として記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。決定1 について KICS の c5b31ab9（MFA delete）を `modules/s3/main.tf` に限って抑止する（repository-wide/ADR-0501 (development-tooling-composition) 決定13）。

## 検討した代替案

### 案A: MFA delete を有効にする

root での操作が要り、bootstrap の実行者を一時 IAM user に限る方針と両立しない。lifecycle と併用できない。

### 案B: Object Lock（governance）を掛ける

lock file の削除が保持期間に当たる。sandbox の複製を destroy できなくなる。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 高 | 高 | 中 |
| Testability | 低（root が要る） | 低（破棄できない） | 高 |
| Contract Clarity | 中 | 低 | 高 |
| Maintainability | 低（lifecycle と排他） | 低 | 高 |
| Scope Control | 低 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

state の版を、書き込みの権限を持つ主体からも守ること。

## 保証範囲

- 保証すること: state Bucket の版管理が `Enabled` であり、上書きされた state の旧版が残ること
- 保証しないこと: bootstrap の段5 より前に、書き込みを apply の role に限ること。書き込みの権限を持つ主体による版の削除を防ぐこと

## 検証方法

- Unit Test: `modules/s3/tests/naming.tftest.hcl` の `bucket_attributes` が、版管理が `Enabled` であることを mock の plan で確かめる
- `modules/s3` は `aws_s3_bucket_object_lock_configuration` を持たず、`aws_s3_bucket_versioning` に `mfa` を渡さない。これを見る検査は無い。Policy Test（#75）が配線されたら、そちらで固定する
- Security: `make kics` が c5b31ab9 を `modules/s3/main.tf` 以外の path で報告しないこと。抑止の宣言は同ファイル先頭の `kics-scan disable` が持つ

## 影響

段5 までの間、state Bucket への書き込みを拒否する宣言は無い。管理アカウントで S3 の書き込み権限を持つ IAM 主体は、誰でも state を上書きできる。

## 見直し条件

- root を使わずに MFA delete を有効にする手段を AWS が提供したとき
- S3 backend が Object Lock の掛かった Bucket での lock file を扱えるようになったとき
