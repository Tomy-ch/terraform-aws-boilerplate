# ADR-0301: registry のスキャンを ENHANCED の継続スキャンとし、このユースケースだけが設定を持つ

- Status: Accepted
- Date: 2026-11-19
- Scope: image-registry
- Related: repository-wide/ADR-0207, repository-wide/ADR-0402, repository-wide/ADR-0604

## Context

Security Hub の ECR.1 は push 時か継続のスキャンを、Inspector.2（FSBP）は Inspector の ECR のスキャン（ENHANCED）を求める。AWS はスキャンの設定を private registry 単位で置くことを推奨し、repository 単位の `PutImageScanningConfiguration` の非推奨化を予告している。registry のスキャンの設定はアカウント×リージョンに1つで、destroy すると BASIC（規則なし）に戻る。ENHANCED の filter に当たらない repository はスキャンされない。Trivy（AVD-AWS-0030）と Checkov（CKV_AWS_163）は repository 単位の `scan_on_push` だけを見る。

## Decision

1. registry のスキャンの設定を `scan_type = "ENHANCED"` とし、規則を1つ置く: `scan_frequency = "CONTINUOUS_SCAN"`、filter は `<project>-<env>/*`（WILDCARD）。
2. repository 単位の `scan_on_push = true` を併記する。道具の要求を値で満たすもので、抑止ではない。
3. このユースケースは、自分が作る repository をすべて覆う registry のスキャンの設定を持つ。この設定はアカウント×リージョンに1つなので、同じアカウントで別の書き手を置く構成を supported にしない（`README.md` の unsupported）。このユースケースは create on push・pull through cache・replication を使わない。
4. registry のスキャンの設定の削除と置換を、許可の宣言なしに通さない（repository-wide/ADR-0604 決定11）。
5. Inspector の有効化はこのユースケースが持たない（`account-baseline`、#22）。

## 検討した代替案

### 案A: BASIC

Inspector.2 からの逸脱になる。言語のパッケージと、push の後に公開された脆弱性を検出しない。

### 案B: ENHANCED の `SCAN_ON_PUSH` だけ

push の後に公開された脆弱性を見ない。

### 案C: filter `*`

このユースケースの外の repository まで同じ書き手が設定し、sandbox の検証の複製と衝突する。

## 保証範囲

- 保証すること: このユースケースが作る全 repository が、継続スキャンの filter に当たること（設定の形として）
- 保証しないこと: Inspector が有効であること（#22）。継続スキャンが push の時点のスキャンを含むこと（AWS の文書で確かめていない）。ENHANCED の下で repository 単位の `scan_on_push` がどう扱われるか。env の root でこのユースケースの外に足された書き手

## 検証方法

- Unit Test: `internal/registry-scanning/tests/scanning.tftest.hcl`
- Policy Test（`plan.image_registry`）: 宣言の外の書き手（`PLAN-ECR-01`）、ENHANCED（`PLAN-ECR-02`）、全 repository が CONTINUOUS_SCAN の filter に当たること（`PLAN-ECR-03`）。`plan.stateful_protection`: 削除と置換（`PLAN-STATE-01`）
- Security: Trivy AVD-AWS-0030、Checkov CKV_AWS_163

## 影響

Inspector の ECR のスキャンの課金（イメージの数と再スキャン）が sandbox を含む各環境アカウントに掛かる。

## 見直し条件

- AWS が repository 単位のスキャンの設定を廃止したとき（決定2 の併記を外す）
- 1つのアカウントに、このユースケースの外の repository を置く必要が生じたとき
