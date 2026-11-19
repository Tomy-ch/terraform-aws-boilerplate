# ADR-0001: 組織の証跡の保管庫を log-archive のアカウントに1つ置き、experimental で始める

- Status: Accepted
- Date: 2026-11-19
- Scope: audit-evidence
- Related: repository-wide/ADR-0102, repository-wide/ADR-0206, audit-evidence/ADR-0101

## Context

監査の証跡は、記録される側の主体が消したり読めなくしたりできない場所に置く必要がある。AWS SRA は、組織の証跡を専用の Log Archive のアカウントに集め、そこを改変できない保管にすることを求める（「Log storage in the Log Archive account MUST be immutable」）。`docs/v1.0-plan/scope.md` 第10.1節は、ログ保管（log-archive）のアカウントを組織の監査ログの保管に充てる。

書き手は、組織の trail（管理アカウントが所有する）と、各アカウントの AWS Config である。書き手の形はまだ動く（GuardDuty の finding の書き出しが加わりうる）。

## Decision

1. 保管の Bucket と鍵は log-archive のアカウントに1組だけ置く。`env` は `log-archive` と、検証の器の `sandbox` だけを受ける。
2. ほかのアカウント（admin・security・dev・stg・prd）に置く構成は持たない。
3. このユースケースは experimental で始める（repository-wide/ADR-0102 (use-case-centric-scope) 決定19-22）。免除されるのは公開契約の後方互換性だけである。
4. stable へ昇格する条件は、次のすべてを満たすことである。
   - log-archive のアカウントへ CI が apply し、その後の CI の plan が変更なしを示す
   - 組織の trail と Config の配信が届き、配信された object に既定の保持が付いていることを確かめる
   - sandbox で apply・検証・destroy が通り、期待残存との照合で余剰が0件になる
   - 配信の途絶の alarm が OK の状態になる
5. log-archive のアカウントへの配線と sandbox の検証のどちらかが、v1.0.0 の版を切るまでに入らないときは、このユースケースを削除する。

## 検討した代替案

### 案A: 各アカウントに保管の Bucket を置く

記録される側のアカウントの管理者が、鍵を止めて自分の記録を読めなくできる。以後の配信も止められる。

### 案B: 管理アカウントに置く

最も残したい管理アカウント自身の操作の記録を、操作した主体が握る。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 低（記録される側が鍵を握る） | 低（同左） | 高 |
| Testability | 中 | 中 | 中（実配信は log-archive の配線の後） |
| Contract Clarity | 低（アカウントごとに契約が散る） | 中 | 高 |
| Maintainability | 低 | 中 | 高 |
| Scope Control | 低 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

アカウントごとに独立した保管庫を持つこと。log-archive のアカウントを失うと、組織の証跡の保管をすべて失う。

## 保証範囲

- 保証すること: `env` が `log-archive` と `sandbox` 以外の値なら plan で落ちること
- 保証しないこと: このユースケースが実際に log-archive のアカウントに置かれること（呼び出す root の配置が決める）

## 検証方法

- Unit Test: `tests/validation.tftest.hcl` の `rejects_other_env` が、ほかの `env` を拒むことを確かめる
- 昇格の条件（決定4）は実環境の観測で確かめる。どのテストも見ない

## 影響

log-archive のアカウントの workload root と、その CI の経路が無い間は、このユースケースはどの root からも呼ばれない。

## 見直し条件

- 決定4 の条件を満たしたとき（stable へ昇格する）
- 決定5 の期限に達したとき
- 組織を複数の保管庫（リージョンごと、規制の区分ごと）に分ける要求が出たとき
