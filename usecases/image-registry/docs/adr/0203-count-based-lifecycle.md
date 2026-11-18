# ADR-0203: lifecycle はタグ付きのイメージを件数だけで消し、件数の短縮を取り返しのつかない変更とする

- Status: Accepted
- Date: 2026-11-19
- Scope: image-registry
- Related: repository-wide/ADR-0207, repository-wide/ADR-0604, image-registry/ADR-0202

## Context

Security Hub の ECR.3 は lifecycle policy の存在を求め、中身を見ない。保持の量に公式の推奨は無く、利用者の rollback の深さとリリースの頻度で決まる（repository-wide/ADR-0207 決定8 の retention requirement）。署名と attestation は OCI 1.1 の referrer として untagged の manifest に置かれうる。lifecycle が subject を消すと、ECR は referrer も消す。

## Decision

1. lifecycle policy は repository ごとに1つで、規則は1つとする: `tagStatus = "tagged"`・`tagPatternList = ["*"]`・`countType = "imageCountMoreThan"`・`countNumber = retained_images`・`action = expire`。
2. untagged の規則と日数の規則を持たない。
3. `retained_images` は既定値を持たない必須の入力で、2 以上の整数とする。下限の 2 は独自判断である（repository-wide/ADR-0207 決定4）。1 では直前の版へ戻せない。
4. 件数を減らす変更を、取り返しのつかない変更とする（repository-wide/ADR-0604 決定12）。消えたイメージは戻らない。`aws_ecr_lifecycle_policy` を保護の対象に宣言し、短縮の許可の宣言なしに通さない。

## 検討した代替案

### 案A: untagged を日数で消す規則を足す

referrer を subject より先に消しうる。digest だけで push したイメージ（タグを持たない）を消し、稼働中の Task の pull を壊しうる。

### 案B: 日数で消す（`sinceImagePushed`）

リリースが止まった repository で、稼働中の唯一の版が消える。

### 案C: archive へ移す（`transition`）

archive のイメージは pull できず、rollback を壊す。最低の保持期間で削除が遅れる。

## 保証範囲

- 保証すること: lifecycle がタグ付きのイメージを新しい順に `retained_images` 件残すこと（規則の形として）
- 保証しないこと: 稼働中の Task が参照するイメージが消えないこと。referrer が件数に数えられないこと

## 検証方法

- Unit Test: `internal/repository/tests/repository.tftest.hcl` の `lifecycle_keeps_retained_tagged_images`、`tests/validation.tftest.hcl` の件数の拒否
- Policy Test（`plan.image_registry`）: 規則の形と件数の下限（`PLAN-ECR-05`）、repository ごとに1つ（`PLAN-ECR-04`）。`plan.stateful_protection`: 件数の短縮（`PLAN-STATE-09`）

## 影響

利用者は、Compute の Service がいつまで古い版を参照しうるか（rollback の深さ）から件数を決める。

## 見直し条件

- ECR が、稼働中の Task が参照するイメージを lifecycle から守る機構を持ったとき
- referrer が `tagged` の件数に数えられると分かったとき
