# ADR-0102: AWS Config を sandbox の外の全アカウント・全リージョンで記録し、log-archive へ配信する

- Status: Accepted
- Date: 2026-11-24
- Scope: account-baseline
- Related: repository-wide/ADR-0204, repository-wide/ADR-0207, repository-wide/ADR-0401, account-baseline/ADR-0001, account-baseline/ADR-0101, account-baseline/ADR-0103

## Context

Security Hub の Config.1（Critical）は、リージョンで Config が有効で、記録が有効な control の要求する resource を覆い、recorder が service-linked role を使うことを検査する。IAM のグローバルな resource の記録は1つのリージョンに限ることを勧める。CIS AWS Foundations Benchmark v3.0.0・v5.0.0 の 3.3 は、全リージョンで Config を有効にすることを求める。AWS SRA は、管理アカウントと各メンバーアカウントの、resource を持つ各リージョンで Config を有効にすることを求める。

customer managed の recorder は、アカウント×リージョンに1つしか作れない。配信先は、log-archive のアカウントの `audit-evidence` の Bucket と鍵である。`audit-evidence` は Config の配信を、アカウントごとの文（`config.amazonaws.com` と `aws:SourceAccount`）で受ける。

issue #86 は、sandbox の Config を常駐させないと決めた（選択肢 C。GuardDuty だけを常駐させる）。理由は次のとおりである。

- テストの資源は掃除で残らず、構成の履歴を残す価値が薄い。変更の主体は組織の trail が、常駐層の変化は定期の plan（#77）が、不審な変更は GuardDuty が見る
- 継続の記録は月 約 $37 の見積もりで（未実測）、sandbox の予算（月1万円）の大半を占める。1日1回の記録は、resource ごとに毎日数えると月 約 $77 で、かえって高い
- 常駐させないと、Config の部分を sandbox で作成・破棄して検証できる。常駐させると recorder がアカウント×リージョンに1つでぶつかる

## Decision

1. Config の recorder・delivery channel・recorder の status・retention を、account-baseline/ADR-0101 (default-regions-per-partition) の集合のすべてのリージョンに置く。
2. recorder は service-linked role（`AWSServiceRoleForConfig`）を使う。SLR の持ち方は account-baseline/ADR-0103 (service-linked-roles-by-arn-reference) が持つ。
3. 記録の範囲はすべての対応する resource とし、IAM のグローバルな resource は provider のリージョン（home）でだけ記録する。記録の頻度は `CONTINUOUS` とする。範囲と頻度は入力にしない。
4. 配信先は入力 `config_delivery`（`audit-evidence` の `bucket_name` と `kms_key_arn`）で受ける。鍵は ARN で受け、alias を受けない。
5. 構成の履歴の保持日数は入力 `config_history_retention_days`（30 以上 2557 以下の整数）で受ける。
6. `env = "sandbox"` では Config を置かない。切り替えは `env` の token から導き、機能のフラグを置かない（repository-wide/ADR-0204 (no-generic-escape-hatch)）。
7. 決定6 を公式推奨からの逸脱として下に記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。

## 検討した代替案

### 案A: sandbox にも Config を常駐させる

月 約 $37（見積もり）が sandbox の予算の大半を占める。recorder がアカウント×リージョンに1つなので、sandbox で作成・破棄する検証ができない。

### 案B: 記録の頻度を `DAILY` にして sandbox にも置く

sandbox は apply と destroy が多く、resource ごとに毎日数えると月 約 $77 でかえって高い（#86 の見積もり）。

### 案C: 常駐させ、テストの外では recorder を止める

止めている間の変更は記録されず、Config.1 も満たさない。停止を拒む SCP とも両立しない。

### 案D: 自前の IAM role と `aws:PrincipalOrgID` で組織の1つの Bucket へ配信する（Config の文書の助言）

Config.1 の SLR の検査で外れる。`audit-evidence` はアカウントごとの文で受けている。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 採用案 |
| --- | --- | --- | --- | --- | --- |
| Security | 高 | 高 | 低 | 中 | 中（sandbox の履歴を持たない） |
| Testability | 低（作成・破棄できない） | 低 | 中 | 中 | 高 |
| Contract Clarity | 高 | 中 | 低 | 中 | 高 |
| Maintainability | 高 | 中 | 低 | 低（role を持つ） | 高 |
| Scope Control | 中 | 中 | 低 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- sandbox の構成の履歴
- 記録の頻度と範囲を利用者が選ぶこと

## 保証範囲

- 保証すること: `sandbox` で Config の module が0件で、ほかの env で1件であること。reference の plan で、Config の4つの型が集合の各リージョンに在り、recorder の `role_arn` が Config の SLR の ARN の形で、status が有効で、グローバルな resource を記録する recorder がちょうど1つのリージョンであること。`config_delivery` が各リージョンの delivery channel に届くこと
- 保証しないこと: 別のアカウントの Bucket へ、SLR の主体で配信が届くこと。`config_history_retention_days` の範囲が API の受ける範囲と一致すること

## 検証方法

- Unit Test: `internal/config-recorder/tests/` の `recorder_per_region` が、SLR の service 名、すべての resource の継続の記録、home だけのグローバルな記録、配信先、status、保持日数を確かめる
- Contract Test: `tests/contract.tftest.hcl` の `workload_account` が Config の合成と配信先を、`sandbox_has_no_config` が sandbox で0件であることを確かめる。`tests/validation.tftest.hcl` が `config_delivery` の鍵の形と、保持日数の範囲を確かめる
- Policy Test（`make tf-policy`）: `PLAN-ABL-01`（各リージョンに在る）、`PLAN-ABL-02`（`role_arn` と status）、`PLAN-ABL-03`（グローバルな記録が1つのリージョン）

## 影響

`audit-evidence` の `writers.config_source_accounts` に、sandbox の外の各アカウントの ID が要る。

## 公式推奨からの逸脱

- 外れる推奨: Security Hub の Config.1 と CIS AWS Foundations Benchmark v3.0.0・v5.0.0 の 3.3（全リージョンで Config を有効にする）、AWS SRA の「MUST be enabled for the management and each member account」。sandbox のアカウントだけが外れる
- 外れる理由: sandbox の継続の記録の費用（月 約 $37 の見積もり、未実測）が sandbox の予算の大半を占め、テストの資源は掃除で残らないので履歴の価値が薄い（#86）
- ユースケース上の制約: sandbox は検証のための使い捨ての資源を置くアカウントで、掃除の CI が資源を消す（`docs/v1.0-plan/scope.md` 第10.2節）。recorder はアカウント×リージョンに1つで、常駐させると sandbox で作成・破棄して検証できない
- 影響: security では、sandbox の構成の履歴が無く、Config で構成の変化を遡れない。変更の主体は組織の trail に、不審な操作は GuardDuty に残る。availability への影響は無い。cost では、sandbox の Config の費用が掛からない
- 代替案: 案A（常駐）、案B（`DAILY`）、案C（常駐させて止める）。上の「検討した代替案」
- 見直し条件: Security Hub を入れるとき。sandbox に掃除の対象外の長く残る資源が増えたとき。Config のルールで検証したい不変条件が出てきたとき

## 見直し条件

- 上の「公式推奨からの逸脱」の見直し条件のいずれか
- 記録の頻度や範囲を変えたい利用者が現れたとき（意味論の入力として足す）
