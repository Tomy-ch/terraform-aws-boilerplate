# ADR-0101: state Bucket を state の鍵による SSE-KMS で暗号化し、backend が鍵の ARN を名指す

- Status: Accepted
- Date: 2026-10-25
- Scope: state-backend
- Related: repository-wide/ADR-0207, repository-wide/ADR-0602, repository-wide/ADR-0501, repository-wide/ADR-0301

## Context

AWS は、S3 の暗号化に customer managed key（CMK）による SSE-KMS を使うと、鍵の権限・ローテーション・監査を制御できるとする。Trivy は CMK を使わない Bucket を AWS-0132 として、Checkov は CKV_AWS_145 として報告する。

Terraform の S3 backend は、`encrypt` が真で `kms_key_id` が無いとき、state と lock file の書き込みに SSE-S3（`AES256`）を明示する。S3 は要求の暗号化の header を Bucket の既定の暗号化より優先するので、Bucket の既定を SSE-KMS にしても、backend が鍵を名指さなければ state は SSE-S3 で書かれる。

bootstrap の段1 は AWS CLI で state Bucket を作り、段2 はそれを `import` block で取り込む（repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定4・6・8）。CLI で作ってよいのは、Terraform が state を保存するために要るものだけである（同 決定5）。CMK と Key Policy はそれに当たらず、既定の暗号化は AWS がすべての Bucket に置く。

## Decision

1. state Bucket の既定の暗号化を、このユースケースの state の鍵（CMK）による SSE-KMS とし、SSE-C を拒否する。S3 Bucket Key を使わない。
2. 鍵（鍵・Key Policy・alias）は、state backend の構成物として identity root の Terraform が作る（repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定1・5。CLI では作らない）。新しいアカウントでは段4 の apply が作る。段4 を終えたアカウントの CI は鍵を作り直せない（deployment-identity/ADR-0201 (enumerated-permissions) 決定3）。
3. backend は `encrypt = true` を保ち、`kms_key_id` に鍵の ARN を init の `-backend-config` で渡す。ARN は init の前に運用機構が alias から求め、Bucket の既定の暗号化が SSE-KMS ならその鍵と一致しなければ失敗する。CI は鍵が無ければ失敗する。
4. 段1 の CLI は既定の暗号化を置かず、段2 は暗号化の設定を取り込まない。段4 の apply が既定の暗号化を置く。鍵が在る前の段2 から段4 の init は、鍵を渡さないことを人が明示して選び（手順は `docs/setup/bootstrap.md`）、その間の state と lock file は SSE-S3 で書かれる。段5 で state を書き換える最初の CI の apply が、それを SSE-KMS で書き直す。
5. 決定4 の期間の SSE-S3 と AWS の既定への依存を、公式推奨からの逸脱として記録する（repository-wide/ADR-0207 (default-value-policy) 決定11。下の「公式推奨からの逸脱」）。
6. Bucket Key を使わないことは、公式推奨が決めていない値の独自判断である（repository-wide/ADR-0207 決定3・4）。state の書き込みは apply と plan ごとに数件で、Bucket Key が減らす KMS の要求と費用は小さい。Bucket Key を使うと、bucket-level key の有効期間は Key Policy を評価せずに復号が続きうり、暗号化コンテキストが Bucket の ARN になって root ごとの state の key を CloudTrail と鍵の条件で区別できなくなる。

## 検討した代替案

### 案A: `kms_key_id` を書かず、`encrypt = false` で Bucket の既定に任せる

Terraform の `kms_key_id` の経路から外れる。新しいアカウントの段4 で、apply の途中で既定が SSE-KMS に変わった後の state の書き込みに、Key Policy に名指されない実行者の鍵の権限が要り、書き込みが落ちる。書き込みが鍵 ID の header を持たないので、特定の鍵を求める Bucket Policy（S3 User Guide）を置けない。

### 案B: `backend.tf` に alias を literal で書く

S3 は鍵を ARN で名指すことを勧める。alias を付け替えられる主体が state の暗号化先を変えられる。鍵が在る前の段4 の apply が lock の取得で落ちる。

### 案C: AWS 管理鍵（`aws/s3`）で SSE-KMS にする

鍵の権限で読み手を絞れない点は SSE-S3 と変わらない。

### 案D: S3 Bucket Key を使う

決定6 の理由で採らない。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 採用案 |
| --- | --- | --- | --- | --- | --- |
| Security | 中（鍵を求める Bucket Policy を置けない） | 中（alias の付け替えで暗号化先が変わる） | 低 | 中（Key Policy の評価が遅れる） | 高 |
| Testability | 中 | 低（段4 が落ちる） | 中 | 高 | 高 |
| Contract Clarity | 低（`encrypt = false` が意図と逆に読める） | 中 | 中 | 中 | 高 |
| Maintainability | 中 | 低（project と env を backend に焼き込む） | 高 | 高 | 中（init に運用機構を通す） |
| Scope Control | 低（Terraform の経路から外れる） | 中 | 中 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 新しいアカウントの段1 から、段5 で state を書き換える最初の CI の apply までの間、鍵の権限で state の読み手を絞ること
- KMS の要求と費用を Bucket Key で減らすこと

## 保証範囲

- 保証すること: state Bucket の既定の暗号化が state の鍵による SSE-KMS で、SSE-C を拒否し、Bucket Key を使わない設定を持つこと。CI の init が鍵の ARN を backend に渡し、鍵が無いか Bucket の既定の鍵と食い違えば init の前に落ちること（いずれも宣言とツールの判定として。実 API での振る舞いは保証しない）
- 保証しないこと: 切り替えの前に書かれた state の版が、lifecycle の日数（state-backend/ADR-0103 (expire-noncurrent-state-versions)）を過ぎるまで SSE-S3 で残ること。鍵を管理できる主体が alias と Bucket の既定を揃えて付け替えること（突き合わせは片方の付け替えだけを検出する）。Key Policy に名指されない主体（一時 IAM user を含む）が state を読むこと —— 段5 の後の回復は unsupported（README）

## 検証方法

- Unit Test: `modules/s3/tests/` が、暗号化の値（`aws:kms`、鍵の ARN、`bucket_key_enabled = false`）、SSE-C の拒否、`kms_key_arn` の validation と precondition を mock の plan で確かめる
- Contract Test: `tests/contract.tftest.hcl` が、2つの Bucket の暗号化が `state_key_arn` の鍵を名指すことを確かめる
- Unit Test（Go）: init に鍵の ARN を渡す運用機構（`scripts/tf-local`）のテストが、alias の有無・Bucket の既定との食い違い・鍵を渡さない明示の有無ごとの出力と失敗を確かめ、alias の名前の写しを identity root のテストの直書きと突き合わせる
- Security: `make trivy-config` が AWS-0132 を、`make checkov` が CKV_AWS_145 を報告しないこと（抑止は持たない）
- 実環境: 切り替えの apply の後の CI の plan が `No changes.` を示し、`get-bucket-encryption` が宣言どおりであること（人、`docs/setup/bootstrap.md`）

## 公式推奨からの逸脱（repository-wide/ADR-0207 (default-value-policy) 決定11）

| 項目 | 内容 |
| --- | --- |
| 外れる推奨 | CMK による SSE-KMS（Trivy AWS-0132、Checkov CKV_AWS_145）。既定値に頼らず明示する（repository-wide/ADR-0207 決定5） |
| 理由 | 段1 の CLI は CMK を作れず（repository-wide/ADR-0602 決定5）、鍵ができるのは段4 の apply である |
| ユースケース上の制約 | bootstrap の段の順序 |
| 影響 | security: 新しいアカウントの段1 から段5 で state を書き換える最初の CI の apply まで、state は SSE-S3 で、段1 から段4 の apply までの既定の暗号化と SSE-C の拒否は AWS の既定による。availability・cost: 無し |
| 代替案 | 案A（検討した代替案） |
| 見直し条件 | 段1 の前に鍵を作れる経路ができたとき。AWS が新しい Bucket の既定を変えたとき |

## 影響

`modules/s3` を使う usecase は、鍵の ARN を必須の入力として渡す。backend を持つ root を足す変更は、その root の init に鍵の ARN を渡す。workload root の apply の role は、alias の一覧と state Bucket の既定の暗号化を読む権限を要る。

## 見直し条件

- workload root を配線するとき（apply の role の読み取りの権限と、Key Policy の利用者）
- 段5 の後の回復の手順を設計するとき
- Bucket Key の有無を変える要件ができたとき
- state-backend 以外の usecase が `modules/s3` を使うとき（鍵の ARN の必須化と Bucket Key を、その usecase の要件で見直す）
- AWS が新しい Bucket の既定の暗号化を変えたとき（決定4・5）
