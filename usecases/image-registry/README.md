# image-registry

発火側（GBp を使うアプリケーションの CI）が push するコンテナイメージを、1つの環境アカウントに保管するユースケース。ECR の private repository と、registry 単位のスキャンの設定と、発火側が assume する push の role を持つ。常駐層であり（`docs/v1.0-plan/scope.md` 第10.2節）、各環境アカウント（dev / stg / prd / sandbox）の workload root から呼ばれる（#94。共有基盤用のアカウントは設けない）。

## 解決する問題

Task 定義が digest で指定するイメージ（`docs/v1.0-plan/scope.md` 第9.4節）の置き場が要る。置き場には次が要る。

- 同じタグが別のイメージを指さないこと（不変タグ）
- push の時点と、その後に公開された脆弱性でイメージが調べられること
- イメージを書けるのが発火側の push の role だけで、イメージを消すのが lifecycle だけであること
- 発火側が長期のアクセスキーを持たずに push できること

## 実行形態とネットワーク境界

AWS のマネージドサービス（ECR）だけで、コードを実行しない。発火側は GitHub Actions の OIDC で push の role を assume し、ECR の公開 endpoint へ push する。Compute の Task が pull する経路（VPC Endpoint の `ecr.api`・`ecr.dkr` と S3 の gateway）は `private-aws-access` が持つ。

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（小文字と数字、20文字以内） | 名前の先頭の token |
| `env` | `string`（`dev` / `stg` / `prd` / `sandbox`） | 名前の2つ目の token。管理アカウントにワークロードを置かないので `admin` を受けない |
| `repositories` | `map(object({ retained_images = number, publisher = object({ owner, owner_id, repository, repository_id, environment }) }))` | キーは論理名（小文字・数字・ハイフン、20文字以内）。repository は `<project>-<env>/<論理名>` になる。`retained_images` はタグ付きのイメージを残す件数（2 以上の整数）。`publisher` はその repository へ push してよい GitHub のリポジトリ（所有者・名前・変わらない数値の ID）と environment |
| `github_oidc_provider_arn` | `string`（GitHub Actions の OIDC provider の ARN） | `deployment-identity` の `github_oidc_provider_arn` |
| `registry_manager_role_arn` | `string`（IAM role の ARN） | このユースケースを apply する role（workload root の apply の role）。repository の暗号化の鍵に、ECR 経由の grant の作成と廃止をこの role に限って許す |

| 出力 | 使う側 |
| --- | --- |
| `repositories` | 論理名ごとの `arn` と `url`。`url` は発火側の push の宛先と、Compute のユースケースの image の入力（digest は発火側が Task 定義の revision に書く。[ADR-0601](../../docs/adr/0601-change-delivery-path.md) 決定20-23）。`arn` は Compute のユースケースの Execution Role の pull の権限の Resource |
| `publisher_role_arns` | 論理名ごとの push の role の ARN。発火側の workflow が assume する |

ログの所在は持たない。ECR は Log Group を持たず、API の記録は CloudTrail（`security-tooling`、`audit-evidence`）が持つ。基盤系アラームの通知先も受けない。基盤系アラームの対象となる metric を持たないためである（`docs/v1.0-plan/scope.md` 第7節）。

### 供給契約（発火側の責務）

- タグは `<version>-<sha>` とする。同じタグを再 push しない。ECR はタグの形を検査しないので、形は発火側が守る。このユースケースが保証するのは、同じタグが別の digest を指さないことだけである
- 署名と attestation は、OCI 1.1 の referrer として同じ repository に置く。タグ方式（`sha256-<digest>.sig`）は使わない
- デプロイしてよいかの判定（スキャン結果、署名の検証）は発火側が行う。push の role はそのためにスキャン結果を読める
- `publisher.environment` に渡す GitHub の environment は、発火側のリポジトリで保護する。required reviewers を置き、deployment branch policy を保護ブランチかタグに限る。push の role の信頼条件は environment の名前までしか見ないので、保護の無い environment は、そのリポジトリに書ける誰でも push できる状態になる

## 持つもの

- 暗号化の鍵（`modules/kms`）。アカウントに1本で、全 repository が共有する
- `repositories` の項ごとに（`internal/repository/`）
  - private repository。不変タグ（`IMMUTABLE`、除外なし）、鍵による KMS 暗号化、push 時のスキャン、`force_delete = false`
  - lifecycle policy。タグ付きのイメージを `retained_images` 件残し、古い順に消す。untagged の規則を持たない（[image-registry/ADR-0203](docs/adr/0203-count-based-lifecycle.md)）
  - repository policy。Allow を持たず、push の role 以外による書き込みと、イメージの削除（`ecr:BatchDeleteImage`）を拒否する（[image-registry/ADR-0202](docs/adr/0202-repository-write-deny.md)）
  - push の role。信頼条件は `publisher` の immutable 形式の `sub` と ID に限り（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定19、[image-registry/ADR-0101](docs/adr/0101-publisher-trust.md)）、権限は push の語彙に限る（[image-registry/ADR-0102](docs/adr/0102-publish-permission-vocabulary.md)）。inline policy と managed policy を排他の resource で固定する
- registry のスキャンの設定（`internal/registry-scanning/`）。`ENHANCED`、`<project>-<env>/*` への `CONTINUOUS_SCAN`（[image-registry/ADR-0301](docs/adr/0301-registry-enhanced-scanning.md)）

## supported / unsupported

supported:

- 1つの環境アカウントに、複数の repository と、それぞれの push の role を置く構成
- 1つの repository に1つの発火側（GitHub のリポジトリと environment の組）
- 署名と attestation を OCI 1.1 の referrer として置くこと

unsupported:

- 別のアカウントからの pull と push（#94。各環境アカウントがそれぞれ repository を持つ）
- タグの上書き（`MUTABLE`）と、タグの除外フィルタ（`IMMUTABLE_WITH_EXCLUSION`）
- タグ方式の署名（`sha256-<digest>.sig`）。不変タグと衝突し、lifecycle の件数にも数えられる
- リージョン間・アカウント間の複製（replication）。env ごとにイメージが違い、各アカウントへ push する。リージョン障害への備えは発火側の再 push
- pull through cache と repository creation template。どちらもこのユースケースの外で repository を作り、既定は `MUTABLE`・AES256 である
- registry のスキャンの設定を、このユースケースの外で持つこと。設定はアカウント×リージョンに1つで、2つの書き手は互いに上書きする
- スキャン結果の通知。所見は Inspector と Security Hub に集まり、その経路は組織の検知（`security-tooling`、#473）が持つ
- Inspector の有効化。#22 が組織の委任管理者として全アカウントで有効にする。このユースケースは registry のスキャンの設定だけを持つ
- GitHub Actions 以外の発火側

## 保証しないこと

- タグの形が `<version>-<sha>` であること（供給契約）
- 稼働中の Task が参照するイメージが消えないこと。`retained_images` 件を超えて新しい版を push すると、Service がまだ参照している版でも消える。push の role を持つ者は、新しいタグを続けて push することで、`ecr:BatchDeleteImage` の拒否を経ずに稼働中の版と戻し先の版を消せる
- 発火側の role の信頼条件の形を、宣言した role の外で確かめること。`plan.publisher_trust` は `conftest/plan/decl.json` の `publisher_trust.roles` の role だけを見る。env の root で足された role も見ない
- 発火側の environment が保護されていること。GitHub の environment の deployment branch policy と承認は、発火側のリポジトリが持つ（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定19）
- 同じアカウントで、このユースケースの外の主体が registry のスキャンの設定を変えること。宣言の外の書き手は Policy Test が reference の plan で見るが、env の root で足された書き手と帯域外の変更は見ない
- Inspector が有効であること（#22）

## 所有者

このユースケースが、鍵・repository・lifecycle policy・repository policy・push の role・registry のスキャンの設定を所有する。Inspector の有効化は `security-tooling`（#473）が、pull の経路の VPC Endpoint は `private-aws-access` が、pull の権限は Compute のユースケースの Execution Role が持つ。

## 依存するユースケースと判断 Issue

- ユースケース: `deployment-identity`（`github_oidc_provider_arn`、apply の role の ARN）
- 外部: GitHub Actions の OIDC（発火側のリポジトリと environment）
- 判断 Issue: #94（ECR を各環境アカウントに置く）、#95（digest の供給経路。[ADR-0601](../../docs/adr/0601-change-delivery-path.md) 決定20-23）
- 掃除 CI（#76）は、このユースケースの鍵・repository・push の role・registry のスキャンの設定を除外する。除外の宣言は掃除 CI が持つ
- workload root の apply の role（`deployment-identity`）が、ECR・IAM・KMS の作成と設定の権限を持つ必要がある。鍵の grant の作成と廃止は Key Policy が許す。IAM の側にも `kms:RetireGrant` が要るか（Amazon ECR User Guide は作成者の IAM policy に要るとする）は確かめていない。その列挙は workload root の apply の経路（#222）が持つ

## 検証方法

- Unit Test（mock）: `internal/repository/tests/` が lifecycle の文書・repository policy・push の role の信頼条件と権限の Resource・入力の検査を、`internal/registry-scanning/tests/` がスキャンの規則を、`tests/naming.tftest.hcl` が名前を、`tests/validation.tftest.hcl` が入力の検査を、`tests/contract.tftest.hcl` が公開 output を、`reference/tests/plan.tftest.hcl` が reference の値を確かめる。鍵の Key Policy の `RolesViaEcr` の文は `modules/kms` のテストが見る
- Policy Test（`make tf-policy`）: `reference/tests/policy-input.tftest.hcl` の plan に対して、`plan.image_registry`（`PLAN-ECR-*`）が registry のスキャンの書き手と ENHANCED と全 repository への継続スキャン、repository ごとに1つの lifecycle policy と repository policy、件数だけで消す lifecycle、`force_delete = false`、Allow の無い repository policy の Deny の形を、`plan.image_registry_publisher`（`PLAN-PUB-*`）が push の role の Action の完全一致と Resource と排他を、`plan.publisher_trust`（`PLAN-PUBTRUST-*`）が信頼条件の形を、`plan.stateful_protection` が repository・lifecycle policy・registry のスキャンの設定の削除と置換と、保持件数の短縮（`PLAN-STATE-09`）と読めない件数（`PLAN-STATE-10`）を確かめる。Policy の一覧は [`conftest/plan/README.md`](../../conftest/plan/README.md)
- Security（`make trivy-config` / `checkov` / `kics` / `semgrep`）: 不変タグ（Trivy AVD-AWS-0031、Checkov CKV_AWS_51、KICS d1846b12）、repository 単位の scan on push（Trivy AVD-AWS-0030、Checkov CKV_AWS_163）、CMK（Checkov CKV_AWS_136、KICS 0e32d561）、public の repository policy（Trivy AVD-AWS-0032、Checkov CKV_AWS_32）、repository policy の存在（KICS 69e7c320）
- Integration（sandbox の実 apply）: 未実施。workload root の apply の経路（#222）と sandbox のアカウント（#21）を待つ。repository・push の role・鍵は、別の `project` の token で複製を作って破棄する（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14 の「複製を作れる」）。registry のスキャンの設定はアカウントに1つで、複製は常駐の設定を上書きするので作らない

検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

- push の Action の過不足。docker・buildx・cosign（referrer 方式）の実 push が、語彙の外の Action を要るかどうか
- ENHANCED の registry の下で、repository 単位の `scan_on_push` がどう扱われるか。plan に差分が出続けないか
- repository policy の Deny（Principal にアカウント）が、アカウントの全 IAM 主体に効くこと。`ecr:BatchDeleteImage` の Deny が lifecycle による削除を止めないこと
- 鍵の Key Policy で、ECR が repository の作成時に grant を作れること。repository の削除で ECR が grant を廃止するのに、apply の role の IAM の側にも `kms:RetireGrant` が要るか
- OCI 1.1 の referrer が、lifecycle の `tagged` の件数に数えられないこと
- 複製の repository が ENHANCED の filter に当たらず、継続スキャンされないこと（Integration の複製は別の `project` の token を持つ）
- 常駐の drift と除外の漏れを見る手段はまだ無い。定期 plan（`terraform-drift.yaml`）が plan するのは env の root だけで、このユースケースを呼ぶ env は無い。掃除 CI（#76）は未配線である

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-19

- AWS / HashiCorp が publisher の ECR の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `ecr` / `ecr-repository` / `container-registry`、`provider=aws` で引いた結果に、namespace `aws`・`aws-ia`・`hashicorp`・`aws-samples` が無い。2026-11-19）
- 参照した一次資料: AWS Provider 6.70.0 の `aws_ecr_repository`・`aws_ecr_registry_scanning_configuration`・`aws_ecr_lifecycle_policy`・`aws_ecr_repository_policy` の文書、Amazon ECR User Guide（[Image tag mutability](https://docs.aws.amazon.com/AmazonECR/latest/userguide/image-tag-mutability.html)、[Image scanning](https://docs.aws.amazon.com/AmazonECR/latest/userguide/image-scanning.html)、[Lifecycle policy parameters](https://docs.aws.amazon.com/AmazonECR/latest/userguide/lifecycle_policy_parameters.html)、[Encryption at rest](https://docs.aws.amazon.com/AmazonECR/latest/userguide/encryption-at-rest.html)、[Pushing an image (IAM)](https://docs.aws.amazon.com/AmazonECR/latest/userguide/image-push-iam.html)）、Security Hub の [ECR の control](https://docs.aws.amazon.com/securityhub/latest/userguide/ecr-controls.html)（ECR.1・ECR.2・ECR.3・ECR.5）と [FSBP](https://docs.aws.amazon.com/securityhub/latest/userguide/fsbp-standard.html)（Inspector.2 を含む）。CIS AWS Foundations Benchmark に ECR と Inspector の項目は無い

採用した知見:

- 不変タグ（ECR.2）。`IMMUTABLE` を明示し、除外を持たない
- registry 単位のスキャンの設定を正にする（Image tag mutability の頁の「private registry 単位で指定するのがベストプラクティス」、`PutImageScanningConfiguration` の非推奨化の予告）。`ENHANCED` で継続スキャン（ECR.1、Inspector.2）
- lifecycle policy を1つ置く（ECR.3）
- push の権限を repository の ARN に限り、`ecr:GetAuthorizationToken` だけを Resource `"*"` の別の文に置く（Pushing an image の単一 repository の例）
- CMK による暗号化（ECR.5）。暗号化の設定は作成後に変えられないので最初から固定する。作成者に `kms:CreateGrant`・`kms:DescribeKey`・`kms:RetireGrant` を `kms:ViaService = ecr.<region>.amazonaws.com` で許す（Encryption at rest）

採用しなかった知見と理由:

- untagged のイメージを日数で消す規則（lifecycle の例の定番）: 署名と attestation の referrer は subject と一緒に消えるので要らず、digest だけで push したイメージを消しうる（[image-registry/ADR-0203](docs/adr/0203-count-based-lifecycle.md)）
- lifecycle の archive（`transition`）: archive のイメージは pull できず rollback を壊す
- `aws_ecr_account_setting`: `ENHANCED` では BASIC の版は効かず、registry policy と blob mounting を使わない
- repository 単位のスキャンの設定だけにする: AWS の推奨から外れる。repository 単位の `scan_on_push = true` は併記し、Trivy AVD-AWS-0030・Checkov CKV_AWS_163 を値で満たす

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

無い。Security Hub の ECR.1・ECR.2・ECR.3・ECR.5 と Inspector.2 に従う。鍵の Key Policy がアカウントへの委任から暗号操作を外すことは、[`modules/kms`](../../modules/kms/README.md) の「公式推奨からの逸脱」が持つ。

Security の道具の抑止（[ADR-0501](../../docs/adr/0501-development-tooling-composition.md) 決定13）:

- Checkov CKV_AWS_393（`internal/repository/publisher.tf` の push の role）: Checkov は GitHub の immutable 形式の `sub`（`repo:<owner>@<id>/<repo>@<id>`）を repo の形と認めない。同じ不変条件（ワイルドカードの不在、`sub` と ID の一致）は `PLAN-PUBTRUST-06`・`08` が見る
- KICS e592a0c5（IAM Access Analyzer）と e38a8e0a（tag）: ほかの module と同じ理由（`account-baseline` の責務、[ADR-0204](../../docs/adr/0204-no-generic-escape-hatch.md) 決定5）。理由と撤回の条件は各ファイルの先頭

## 版

- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` と `override_resource` の要求であり、実行して確かめるのは `mise.toml` の版だけである
- AWS Provider は `>= 6.68.0, < 7.0.0`。reference の lock の版は `providers/` の正本が持つ
