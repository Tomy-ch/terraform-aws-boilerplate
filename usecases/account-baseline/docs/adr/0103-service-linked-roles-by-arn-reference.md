# ADR-0103: このユースケースは、AWS Config の記録に要る service-linked role だけを持つ

- Status: Accepted
- Date: 2026-11-24
- Scope: account-baseline
- Related: repository-wide/ADR-0301, account-baseline/ADR-0102

## Context

account-baseline が置く設定のうち、AWS Config の recorder は、使う role の ARN を `role_arn` に要る。Config.1 は、その role が service-linked role（`AWSServiceRoleForConfig`）であることを検査する。ほかの設定（Access Analyzer の analyzer、EBS・S3・VPC・SSM・EMR のアカウントの設定）は、role の ARN を引数に取らない。

service-linked role はアカウントに1つで、ほかのユースケースの初回の apply が前提にするものもある（AWS Chatbot の SLR）。#472 は、Chatbot の SLR の持ち主を identity root（`deployment-identity`）と決めた。

## Decision

1. account-baseline が作る SLR は、その ARN を account-baseline の resource の引数に渡すものだけとする。Config の SLR（`config.amazonaws.com`）がこれに当たる。
2. ARN を引数に渡さない SLR は、Terraform で作らない。
3. ほかのユースケースが前提にする SLR は、account-baseline が持たない。Chatbot の SLR は `deployment-identity` が持つ。
4. 持つ SLR の型（`aws_iam_service_linked_role`）は、削除と置換を Policy Test で止める型に含める（`conftest/plan/decl.json` の `stateful_protection.protected_types`）。

## 検討した代替案

### 案A: account-baseline が使うすべてのサービスの SLR を持つ

ARN を参照しない SLR を宣言しても、どの resource もそれを使わない。サービスが作った SLR と宣言がぶつかりうる。

### 案B: Config の recorder に自前の IAM role を渡す

Config.1 の SLR の検査で外れる。role を作ると permissions boundary が要り、Config の権限を AWS managed policy で渡すなら repository-wide/ADR-0301 (least-privilege-policy) 決定10 の例外の記録が、渡さないなら customer managed policy の保守が要る。

### 案C: Chatbot の SLR も account-baseline が持つ

issue #472 で退けた。Chatbot の SLR を前提にする `ops-notification` の初回の apply より前に、identity root で作る。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中 | 中 | 中 | 中 |
| Testability | 中 | 中 | 中 | 高 |
| Contract Clarity | 中 | 低 | 中 | 高 |
| Maintainability | 低（使わない宣言） | 低（role を持つ） | 中 | 高 |
| Scope Control | 低 | 中 | 低（他の usecase の前提を持つ） | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

サービスが作る SLR を Terraform の state で追うこと。

## 保証範囲

- 保証すること: Config の SLR の service 名が `config.amazonaws.com` であること。reference の plan で、recorder の `role_arn` が Config の SLR の ARN の形であること。`aws_iam_service_linked_role` が `stateful_protection.protected_types` に在ること
- 保証しないこと: Config の SLR が既に在るアカウントで、SLR の作成が通ること。ARN を引数に渡さない SLR を、サービスが作ること。SLR の削除と置換を止めること。`plan.stateful_protection` は CI の実 plan に掛かり、reference の plan は作成しか持たないので、account-baseline を呼ぶ env の root が配線されるまで、どの plan も SLR の削除と置換を見ない。mock の SLR の ARN は手書きで、実 provider の値の形と一致することを、どの層も見ない

## 検証方法

- Unit Test: `internal/config-recorder/tests/` の `recorder_per_region` が SLR の service 名を確かめる
- Policy Test（`make tf-policy`）: `PLAN-ABL-02` が reference の plan で recorder の `role_arn` の形を見る。`conftest/plan/stateful_protection_test.rego` が、宣言の `protected_types` から SLR の型が外れることを落とす。`PLAN-STATE-*` は CI の実 plan（`plan.stateful_protection`）で SLR の削除と置換を見る

## 影響

Config の SLR が既に在るアカウント（管理アカウントで手で Config を有効にした場合など）では、apply の前に在るかを確かめる。

## 見直し条件

- account-baseline の resource が、ほかの SLR の ARN を引数に要るようになったとき
- Config.1 が SLR の検査をやめたとき
