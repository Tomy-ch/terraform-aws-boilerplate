# ADR-0102: push の role の権限を、push とスキャン結果の読み取りの語彙に固定する

- Status: Accepted
- Date: 2026-11-19
- Scope: image-registry
- Related: repository-wide/ADR-0301, repository-wide/ADR-0402, repository-wide/ADR-0601

## Context

push の role は、本番で動くイメージを置ける資格情報である。push 以外の Action があれば、不変タグの効果を外せる（`ecr:BatchDeleteImage` でタグを消してから同じタグで push する）か、設定を緩められる（`ecr:PutImageTagMutability`・`ecr:SetRepositoryPolicy`）か、このユースケースの外で repository を作れる（`ecr:CreateRepository`）。repository-wide/ADR-0601 (change-delivery-path) 決定22 は、発火側の資格情報を決定20 の操作と、成果物の push と、デプロイの前のスキャン結果の読み取りに要るものに限る。

## Decision

1. push の role の Allow を次の2文に固定する。利用者に選ばせない。
   - repository の ARN に、`ecr:BatchCheckLayerAvailability`・`ecr:BatchGetImage`・`ecr:CompleteLayerUpload`・`ecr:InitiateLayerUpload`・`ecr:PutImage`・`ecr:UploadLayerPart`（Amazon ECR User Guide の単一 repository への push の例）と、`ecr:DescribeImages`・`ecr:DescribeImageScanFindings`（発火側がデプロイの前にスキャン結果を読む。`docs/v1.0-plan/scope.md` 第9.4節）
   - Resource `"*"` に `ecr:GetAuthorizationToken`。この Action は resource-level の権限を持たない（Service Authorization Reference。repository-wide/ADR-0301 決定9）
2. 上の集合と完全一致で検査する。拒否の一覧で見ない。AWS が足す Action を見逃すためである。
3. role の inline policy をこの1つに、managed policy を空に、排他の resource で固定する。

## 検討した代替案

### 案A: 利用者が権限の語彙を選ぶ（`["push", "read-findings"]`）

選択肢が実質1つで、つまみが増えるだけである。

### 案B: AWS の全 repository への例（`repository/*`）

repository ごとに role を分ける意味が無くなる。

## 保証範囲

- 保証すること: push の role が、語彙の外の ECR の Action と、ほかの repository への Action を持たないこと
- 保証しないこと: 語彙が docker・buildx・cosign の実 push に過不足なく足りること（実 push でしか見えない）

## 検証方法

- Policy Test（`plan.image_registry_publisher`）: Allow の Action の集合の完全一致（`PLAN-PUB-01`）、push の Action の Resource が同じ instance の repository の ARN だけであること（`PLAN-PUB-02`）、Resource `"*"` の文が `ecr:GetAuthorizationToken` だけであること（`PLAN-PUB-03`）、排他の resource（`PLAN-PUB-04`）
- Policy Test（`plan.iam_action_wildcard`）: `ecr:GetAuthorizationToken` は `conftest/plan/decl.json` の `resource_wildcard_actions` に宣言する（`PLAN-IAM-03`）
- Integration（sandbox）: 実 push で語彙が足りること。未実施（`README.md` の検出できない故障モード）

## 影響

発火側は、スキャン結果を `DescribeImageScanFindings` で読める。ENHANCED の所見を読むのに Inspector の権限が要るかは確かめていない。

## 見直し条件

- 実 push で語彙の外の Action が要ると分かったとき
- 発火側がスキャン結果を Security Hub か Inspector から読むと決めたとき
