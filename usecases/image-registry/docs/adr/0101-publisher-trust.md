# ADR-0101: push の role の信頼条件を、発火側の immutable 形式の environment の sub と ID で限定する

- Status: Accepted
- Date: 2026-11-19
- Scope: image-registry
- Related: repository-wide/ADR-0602, repository-wide/ADR-0402, deployment-identity/ADR-0101

## Context

repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定19 は、本リポジトリの外のリポジトリの CI（発火側）が AWS を操作する role にも、決定14 の第1項（`aud`、`sub` が含むリポジトリと environment 名、リポジトリと所有者の変わらない ID の完全一致）と決定15（ワイルドカードの禁止）を当て、限定に使う値を型付きの入力として受けることを求める。push の role は、このユースケースで最初の発火側の role である。

## Decision

1. push の role の信頼条件は1文とし、Principal を入力 `github_oidc_provider_arn` の OIDC provider、Action を `sts:AssumeRoleWithWebIdentity`、条件演算子を `StringEquals` だけとする。
2. 条件キーと値は deployment-identity/ADR-0101 (trust-by-immutable-environment-subject) 決定2 と同じ4つとし、値を入力 `repositories[*].publisher` から組む。`sub` は `repo:<owner>@<owner_id>/<repository>@<repository_id>:environment:<environment>` である。
3. `publisher` は所有者・名前・2つの数値の ID・environment 名を別々の値で受け、claim の文字列を受けない。各値は GitHub が名前に許す文字に限り、`*`・`?`・`:`・`/`・`@` を受けない。
4. environment 名は入力で受ける。deployment-identity と違い、発火側の environment の名前と保護は発火側のリポジトリが持つ（repository-wide/ADR-0602 決定19）。
5. 1つの repository に1つの role と1つの発火側を対応させる。

## 検討した代替案

### 案A: claim の文字列（`sub`）を入力で受ける

形の検査が文字列の正規表現だけになり、ワイルドカードや ref 形式を validation で漏れなく閉じる必要がある。AWS と GitHub の表現をそのまま受ける Generic Escape Hatch に近い（repository-wide/ADR-0204 決定2）。

### 案B: 旧形式の `sub`（`repo:<owner>/<repo>:environment:<env>`）

所有者かリポジトリの名前が付け替えられると、別のリポジトリが同じ `sub` を得うる。ID の条件キーで補えるが、`sub` そのものが一意でない。

### 案C: 発火側をアカウントの1つの role にまとめる

repository ごとの Resource の限定と、repository policy の Deny の例外（image-registry/ADR-0202）が1つの role に集まり、1つの発火側が奪われると全 repository へ push できる。

## 保証範囲

- 保証すること: push の role を assume できるのは、入力のリポジトリ・所有者・environment の組の GitHub Actions の job だけであること（信頼条件の形として）
- 保証しないこと: 発火側の environment が保護されていること。発火側のリポジトリの workflow の内容

## 検証方法

- Unit Test: `internal/repository/tests/repository.tftest.hcl` の `publisher_trust_is_immutable_environment_sub` が条件キーと値と Principal を、`tests/validation.tftest.hcl` がワイルドカードと数字でない ID の拒否を見る
- Policy Test（`plan.publisher_trust`）: 文の数、Action、Principal が GitHub Actions の OIDC provider であること、`StringEquals` だけであること、4つのキー、ワイルドカードの不在、`aud`、`sub` の形と ID の一致（`PLAN-PUBTRUST-01`-`08`）
- Checkov の CKV_AWS_393 は immutable 形式の `sub` を認めないので抑止し、上の Policy Test を権威とする（`README.md` の「公式推奨からの逸脱」）

## 影響

発火側は、GitHub の environment を作り、その名前と、リポジトリと所有者の ID を利用者へ渡す。発火側のリポジトリが旧形式の `sub` のままなら、immutable 形式への opt-in が要る。

## 見直し条件

- GitHub が immutable 形式の `sub` を変えたとき
- GitHub Actions 以外の発火側を扱うとき
