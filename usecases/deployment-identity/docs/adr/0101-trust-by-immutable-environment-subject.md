# ADR-0101: 信頼条件を immutable 形式の environment の sub と、リポジトリと所有者の ID で限定する

- Status: Accepted
- Date: 2026-10-25
- Scope: deployment-identity
- Related: repository-wide/ADR-0602, repository-wide/ADR-0402, repository-wide/ADR-0206, repository-wide/ADR-0102

## Context

repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定14 は、各 role の信頼条件を GitHub Actions の OIDC の environment claim で限定し、`aud`、リポジトリ、environment 名、リポジトリと所有者の変わらない ID を完全一致で見ることを決め、用いる claim と AWS の条件キーを use-case の ADR に記録することを求める。同 決定15 はワイルドカードを禁じる。

GitHub は、2026-07-15 より後に作られたリポジトリの `sub` を、所有者とリポジトリの名前に数値の ID を添えた immutable 形式にする。それより前のリポジトリは、opt-in するまで旧形式（`repo:<owner>/<repo>:...`）のままである。GitHub の文書が immutable 形式の例として示すのは `ref` 形式の `sub` だけで、environment 形式の例は旧形式のものしか無い。

## Decision

1. 信頼条件は1文とし、Principal を当該アカウントの GitHub Actions の OIDC provider、Action を `sts:AssumeRoleWithWebIdentity` とする。条件演算子は `StringEquals` だけを使う。
2. 条件キーと値を次に固定する。条件キーは、IAM User Guide の「Available keys for OIDC federation」の GitHub の表にあるものに限る。`repository_id` と `repository_owner_id` は同表で信頼条件にだけ使える（Available in session が No）。

   | 条件キー | 値 |
   | --- | --- |
   | `token.actions.githubusercontent.com:aud` | `sts.amazonaws.com` |
   | `token.actions.githubusercontent.com:sub` | `repo:<owner>@<owner_id>/<repo>@<repo_id>:environment:<environment>` |
   | `token.actions.githubusercontent.com:repository_id` | リポジトリの ID |
   | `token.actions.githubusercontent.com:repository_owner_id` | 所有者の ID |

3. environment 名は `<env>-plan` / `<env>-apply` / `<env>-apply-identity` に固定し、入力にしない。role ごとに environment を分けることを、利用者が崩せないようにする。
4. OIDC provider の audience は `sts.amazonaws.com` の1つに限り、`thumbprint_list` を設定しない。
5. 旧形式の `sub` と GitHub Enterprise Server を扱わない（`README.md` の unsupported）。
6. deployment branch policy は `GITHUB_REF` に対して照合され、Pull Request を契機とする job の ref は `refs/pull/<番号>/merge` である（GitHub の文書）。plan の environment は保護ブランチに加えてこの ref を許し、apply と apply-identity の environment は許さない。environment の宣言は、保護設定の宣言（repository-wide/ADR-0603 (branch-protection-and-required-checks) 決定4）が持つ。
7. environment 形式の `sub` の形（決定2）は、旧形式の environment 形式と、immutable 形式の `ref` 形式の例からの推論であり、実 token の claim で確かめるまで仮説として扱う。確かめた claim は本 ADR に記録する。

## 検討した代替案

### 案A: `StringLike` と `repo:<owner>/<repo>:*` の形（AWS の文書の例）

リポジトリの中のどの job でも assume できる。repository-wide/ADR-0602 決定14・15 に反する。

### 案B: `token.actions.githubusercontent.com:environment` を `sub` と併記する

`sub` の environment の部分と同じ token の同じ値であり、保証が増えない。値を2箇所で揃える負担が増える。

### 案C: 旧形式と immutable 形式の両方を受ける

信頼条件の形が2つになり、Policy Test の期待も2つになる。旧形式のリポジトリは opt-in で immutable 形式にできる。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 低（リポジトリの全 job） | 高 | 中（名前の再取得に弱い形が残る） | 高 |
| Testability | 中 | 中 | 低（期待が2つ） | 高 |
| Contract Clarity | 低 | 中 | 低 | 高 |
| Maintainability | 高 | 中 | 低 | 高 |
| Scope Control | 中 | 高 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 旧形式の `sub` のリポジトリと GitHub Enterprise Server で、そのまま使えること
- environment の名前を利用者が選べること

## 保証範囲

- 保証すること: 信頼条件の文書が、入力から決定2 の形に組まれること（Unit Test）
- 保証しないこと: 決定2 の `sub` の形が実 token と一致すること（決定7 の確認まで）

## 検証方法

- Unit Test: `internal/github-oidc/tests/trust.tftest.hcl` が、3つの role の信頼条件の文書を完全一致で、`tests/validation.tftest.hcl` が名前に `*` `?` `:` を含む入力の拒否を確かめる
- Policy Test（`make tf-policy`）: repository-wide/ADR-0602 の検証方法の、信頼条件のワイルドカードの不在、environment 形式、完全一致の項目
- 運用機構（`make repo-settings-check`）: environment 名が environment の宣言に存在すること
- 実 token: 決定7。最初の CI の plan が role を assume できることで確かめる（`docs/setup/bootstrap.md` の段4）

## 影響

派生したリポジトリは、`sub` の形式を immutable にしてから使う（`docs/setup/bootstrap.md` の段3）。

## 見直し条件

- 実 token の `sub` が決定2 の形と違ったとき
- GitHub が environment 形式の `sub` を廃止または変更したとき
- AWS が GitHub の OIDC の条件キーを変えたとき
