# ADR-0101: 証跡の各版を Object Lock の既定の保持でロックし、同じ日数で失効させる

- Status: Accepted
- Date: 2026-11-19
- Scope: audit-evidence
- Related: repository-wide/ADR-0207, repository-wide/ADR-0604, audit-evidence/ADR-0001, audit-evidence/ADR-0102

## Context

S3 Object Lock は、保持の期間のあいだ object の版の上書きと削除を禁じる。COMPLIANCE は root を含めて誰も期間を短縮できず、GOVERNANCE は `s3:BypassGovernanceRetention` を持つ主体が解除できる（Amazon S3 User Guide「Locking objects with Object Lock」）。CloudTrail と Config は配信する object に保持を指定しないので、Bucket の既定の保持が無いと、配信された証跡はすぐに消せる。Object Lock は Bucket の作成時にしか有効にできず、有効にした後は止められない。

COMPLIANCE の object は保持が切れるまでアカウントを閉じる以外に消せず、sandbox で使うと掃除で消せない費用になる（`docs/v1.0-plan/scope.md` 第10.2節）。

## Decision

1. Bucket は Object Lock を有効にして作り、版管理を `Enabled` にする。
2. Bucket の既定の保持を必ず置く。mode は `env` から導く: `log-archive` は `COMPLIANCE`、`sandbox` は `GOVERNANCE`。mode を入力にしない。
3. 保持の日数は入力 `retention_days` で受け、365 以上 3653 以下の整数に限る。下限は監査の証跡を短く保持する誤りを、上限は打ち間違いで取り消せない長期の保持を掛ける誤りを防ぐ。`sandbox` は入力を使わず1日にする。
4. lifecycle は、現行版を保持と同じ日数で失効させ、上書きされた版を1日で、版の無い delete marker と中断した multipart upload を消す。保持中の版は lifecycle でも消えないので、失効は保持の切れた版にだけ効く。
5. `sandbox` だけ `force_destroy` を有効にする。

## 検討した代替案

### 案A: mode を入力にする

SRA と Security Hub の S3.15 が COMPLIANCE を求める本番の保管で、GOVERNANCE を選べる口が残る。GOVERNANCE では bypass の権限を持つ主体が実質の改変可能者になる。

### 案B: 保持と失効を別の入力にする

「保持は短く、保管は長く」の要求が無い。失効を保持より短くすると、現行版が保持中に delete marker で隠れる。

### 案C: 失効を持たない

保管の費用が際限なく残り、Security Hub の S3.10・S3.13 から外れる。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中 | 高 | 高 | 高 |
| Testability | 高 | 中 | 高 | 高 |
| Contract Clarity | 低（取り違えうる口） | 低 | 高 | 高 |
| Maintainability | 中 | 低 | 低（費用が残る） | 高 |
| Scope Control | 中 | 低 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

保持より長く保管すること、保管の途中で安い storage class へ移すこと。

## 保証範囲

- 保証すること: log-archive では既定の保持が COMPLIANCE と `retention_days`、sandbox では GOVERNANCE と1日になること。現行版の失効の日数が保持の日数と一致すること。`retention_days` の範囲の外が plan で落ちること
- 保証しないこと: 配信された object に実際に既定の保持が付くこと。Bucket の作成の後に `object_lock_enabled` を変えたときに置換になるか

## 検証方法

- Unit Test: `tests/derivation.tftest.hcl` が env からの mode・日数・`force_destroy` の導出を、`internal/archive-bucket/tests/bucket.tftest.hcl` と `lifecycle.tftest.hcl` が既定の保持と lifecycle の値を、`tests/validation.tftest.hcl` が日数の範囲を確かめる
- Policy Test: `PLAN-AUDIT-01`（既定の保持があり、失効の日数と一致する）、`PLAN-AUDIT-04`（Object Lock が有効）。保持の日数の短縮は、同じ日数を持つ lifecycle の短縮として `PLAN-STATE` が CI の plan で拒む
- 配信された object の保持は実環境で `GetObjectRetention` を引いて確かめる。どのテストも見ない

## 影響

`retention_days` を短くしても、既に書かれた object の保持は短くならない。log-archive の Bucket は保持中の object が残る限り destroy できない。

## 見直し条件

- 保持と保管を分ける要求が出たとき
- Security Hub の S3.15 の `mode` parameter を組織で使うとき
