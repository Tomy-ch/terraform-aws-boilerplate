# ADR-0103: state Bucket の上書きされた版を、保持日数で消す

- Status: Accepted
- Date: 2026-11-08
- Scope: state-backend
- Related: repository-wide/ADR-0102, repository-wide/ADR-0207, repository-wide/ADR-0604, state-backend/ADR-0102

## Context

state Bucket は版管理を持つ（state-backend/ADR-0102 (no-mfa-delete-or-object-lock)）。state の上書きと、S3 backend が lock を外すたびに消す lock file（`<key>.tflock`）は、上書きされた版と delete marker を残す。lifecycle が無いと、どちらも際限なく溜まる。Security Hub の S3.10 は、版管理を持つ Bucket に lifecycle を求める。

上書きされた版は、壊れた state を戻すための回復点である。何日残せば足りるかは、apply の頻度と、壊れた state に気づくまでの日数で決まり、利用者ごとに違う。AWS と HashiCorp は state Bucket に版管理を推奨するが、上書きされた版の保持日数の推奨値を持たない。

## Decision

1. state Bucket の lifecycle は、Bucket 全体（空の filter。identity と workload の state と lock file を含む）に掛かり、上書きされた版を保持日数で消し、失効した delete marker を消し、未完了の multipart upload を1日で中止する。
2. 現行版を失効させる規則と、storage class を移す規則を持たない。
3. 保持日数は公開入力 `state_noncurrent_version_retention_days`（1以上の整数）とし、既定値を置かない。
4. 残す版の数（`newer_noncurrent_versions`）で版を残さない。
5. 規則を `modules/s3` に、入力で resource や block の有無を切り替えずに置く（repository-wide/ADR-0102 (use-case-centric-scope) 決定15）。
6. 未完了の multipart upload を中止する1日は、公式推奨の値ではない独自判断である（repository-wide/ADR-0207 (default-value-policy) 決定4）。S3 backend は大きな state を multipart で置き、未完了の upload は失敗からしか生じず、それを再開する主体は無い。
7. 保持日数を短くする変更と、現行版を失効させる規則を足す変更は、root ごとの短縮の許可の宣言に address と理由を足さない限り、CI の plan と apply で落とす（Policy Test）。lifecycle の削除と置換も、state Bucket と同じく保護の対象にする（repository-wide/ADR-0604 (state-split-by-apply-role) 決定11・12）。

## 検討した代替案

### 案A: lifecycle を持たない

上書きされた版と lock file の delete marker が際限なく溜まる。bootstrap の段5 で SSE-KMS へ切り替えた後も、切り替えの前に書かれた SSE-S3 の版が消えない。Security Hub の S3.10 を満たさない。

### 案B: 保持日数に既定値を置く

消した版は戻らない。既定値を置くと、版を上げただけの利用者の回復点が、選んでいない日数で消え始める。有限の値はどれも回復点を消すので、安全側の既定は「消さない」しか無く、それは規則の block の有無を入力で切り替えることになる（決定5）。

### 案C: 残す版の数でも残す（`newer_noncurrent_versions`）

apply の少ない key で直前の版が日数を過ぎても残り、回復には有利になる。一方、版の数に満たない key では、SSE-S3 の版と delete marker が日数を過ぎても消えない。版の数も独自判断の値になる。

### 案D: key の prefix ごとに規則を分ける

state の key の配置（repository-wide/ADR-0604 (state-split-by-apply-role)）を lifecycle に写すことになり、key を足す変更が lifecycle も変える必要を持つ。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 採用案 |
| --- | --- | --- | --- | --- | --- |
| Security | 低（旧い暗号化の版が残る） | 中 | 中（版が残り続ける key がある） | 中 | 高 |
| Testability | 高 | 高 | 中 | 中 | 高 |
| Contract Clarity | 高 | 低（選んでいない日数で消える） | 低（2つの値の組み合わせ） | 低 | 高 |
| Maintainability | 低（版が溜まる） | 中 | 中 | 低（key の配置と二重） | 高 |
| Scope Control | 中 | 中 | 中 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

保持日数を過ぎた版へ state を戻すこと。

## 保証範囲

- 保証すること: state Bucket が、現行版を失効させず、上書きされた版を `state_noncurrent_version_retention_days` の日数で消し、失効した delete marker を消し、未完了の multipart upload を1日で中止する規則を持つこと（宣言として）。保持日数を短くする変更と現行版の失効を足す変更が、許可の宣言なしには CI の apply に至らないこと
- 保証しないこと: lifecycle が日数どおりに版を消すこと（S3 は失効を非同期に行う）。許可を宣言した短縮と lifecycle の作成が、その時点で日数を過ぎた版を消すのを防ぐこと

## 検証方法

- Unit Test: `modules/s3/tests/lifecycle.tftest.hcl` が、規則の各要素の値、filter が tag で絞らないこと、現行版を日付で失効させないこと、版の数で残さないこと、入力の検査を mock の plan で確かめる。現行版を日数で失効させないこと、filter が prefix と大きさで絞らないこと、規則が state Bucket に付くことは mock の apply で確かめる。既定値が無いことを見る検査は無い
- Contract Test: `usecases/state-backend/tests/contract.tftest.hcl` が、公開入力の検査と、値が規則へ届くことを確かめる
- Policy Test: `plan.stateful_protection` の `PLAN-STATE-04`・`05` が、日数の短縮と、使われない短縮の許可を落とす。Rego のテストが、短縮・失効の追加・規則の改名・有効化・置換と、延長・作成・無効化を固定する
- Security: `make checkov` が `modules/s3` で CKV2_AWS_61 と CKV_AWS_300 を報告しないこと

## 影響

保持日数を短くする変更は、同じ変更に短縮の許可の宣言を含めたときだけ、merge と CI の apply（apply-identity の role）を経て、日数を過ぎた版を消す。plan の出力の Bucket の lifecycle も同じ型なので、`plan_output_retention_days` を短くする変更にも許可が要る。apply の role は state Bucket の設定を変えられない（repository-wide/ADR-0604 決定10）。lifecycle による失効は S3 が行い、Bucket Policy の object への Deny では止まらない。lifecycle を変えられる主体を限ることで統制する。

## 見直し条件

- 回復の要件が、日数ではなく版の数で表されるとき
- state Bucket に Object Lock を掛けるとき（state-backend/ADR-0102 の見直し条件）
