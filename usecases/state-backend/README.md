# state-backend

Terraform の state を置く S3 Bucket と、CI の plan の出力を置く S3 Bucket を、1つのアカウントに1つずつ持つユースケース。bootstrap の構成物であり（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）、各 env の identity root から呼ばれる（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定3・5）。

## 解決する問題

state は、そのアカウントを変更できる主体だけが書ける場所に、版を残して置かなければならない。state backend は Terraform より先に存在する必要があるので、bootstrap の段1 で AWS CLI が作り、段2 で Terraform が取り込む（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定4-8）。このユースケースは、取り込んだ後の Bucket を Terraform の Desired State に置き、以後の変更を CI の apply-identity の role に限る。

CI の plan の全文は公開されるログに出さず、state Bucket と分けた Bucket へ plan の role が書く（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定13）。その Bucket もこのユースケースが持ち、保持日数を過ぎた版を lifecycle で消す（[state-backend/ADR-0201](docs/adr/0201-plan-output-bucket.md)）。

## 実行形態とネットワーク境界

- Terraform の S3 backend が、CI のランナーと bootstrap の実行者の手元から、AWS の公開 API の endpoint で読み書きする。AWS の上で動く処理は無い
- 外への egress は無い。2つの Bucket は、どちらも Public Access Block の4項目をすべて有効にし、ACL を無効にする（`BucketOwnerEnforced`）
- TLS でない要求は、Bucket Policy の Deny で拒否する
- state Bucket の object の書き込みは、Bucket Policy の Deny で CI の apply の2つの role と lock の解除の role に限り、各 role を自分の key と操作に閉じ込める。state の鍵以外で暗号化する書き込みも拒否する（[state-backend/ADR-0104](docs/adr/0104-state-object-write-deny.md)）

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字） | 名前の先頭に置くプロジェクトの token |
| `env` | `dev` / `stg` / `prd` / `sandbox` / `admin` | Bucket を置くアカウントの env の token（[scope.md 第3節](../../docs/v1.0-plan/scope.md)） |
| `state_noncurrent_version_retention_days` | `number`（1以上の整数、既定値なし） | 上書きされた state の版を残す日数。この日数を過ぎた版は消え、その版へは戻せない。現行版は失効させない（[state-backend/ADR-0103](docs/adr/0103-expire-noncurrent-state-versions.md)） |
| `plan_output_retention_days` | `number`（1以上の整数、既定7） | plan の出力を残す日数。現行版はこの日数で失効し、上書きされた版と失効した版はさらにこの日数で消える |
| `ci_role_arns` | `object({ plan, apply, apply_identity, unlock_identity })`（IAM role の ARN、4つは互いに別の role、既定値なし） | CI の role。state の鍵の Key Policy は、`plan`（CI の plan）と `apply_identity`（identity root の apply）に S3 経由の復号と鍵の生成を、`unlock_identity`（identity root の lock を外す role）に identity root の lock file（`<state Bucket の ARN>/identity/terraform.tfstate.tflock`）の復号だけを、暗号化コンテキスト `aws:s3:arn` で限って許す。state Bucket の Bucket Policy は、`apply`（workload root の apply）に workload root の state と lock file の書き込みを、`apply_identity` に identity root の state と lock file の書き込みを、`unlock_identity` に identity root の lock file の削除だけを許し、ほかの主体の object の書き込みを拒否する。鍵と同じアカウントの role でなければ、`modules/kms` の precondition が落とす。ARN が plan で決まるときは plan で、同じ apply で作る role を渡すときは apply で落ちる |
| `bootstrap_executor_user_name` | `string`（path を持たない IAM user の名前、既定 `null`） | 新しいアカウントの bootstrap で段4 の apply を実行する IAM user。値が在れば、この user は state Bucket の identity root の state と lock file に書き込め、暗号化の方式の header の拒否の例外にも入る。段3 で入れ、段5 の最初の変更で `null` に戻す（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md)、[state-backend/ADR-0104](docs/adr/0104-state-object-write-deny.md) 決定7） |

`state_noncurrent_version_retention_days` と `ci_role_arns` は既定値を持たない必須入力で、足したことは利用者の入力の変更を要する破壊的変更である（[ADR-0202](../../docs/adr/0202-internal-topology-as-implementation-detail.md) 決定4）。

state Bucket の完全な名前は `<project>-<env>-tfstate-<account_id>-<region>-an`、plan の出力の Bucket は `<project>-<env>-tfplan-<account_id>-<region>-an` で、63文字を超えると plan で落ちる。`env = sandbox`・region が `ap-northeast-1` のとき、`project` は16文字までになる。長さの超過と、S3 が予約している接頭辞（`project = "sthree"` のとき `sthree-`）は、`modules/s3` と `internal/plan-output` が内部の入力 `name` の誤りとして plan で落とす。

| 出力 | 使う側 |
| --- | --- |
| `bucket_name` | identity root と workload root の backend の `bucket`、段2 の `import` block の id |
| `bucket_arn` | [`deployment-identity`](../deployment-identity/README.md)。CI の role の IAM で、S3 の Action の Resource を state Bucket とその key に絞る |
| `state_key_arn` | [`deployment-identity`](../deployment-identity/README.md)。CI の role の鍵の権限と、apply の role の鍵の Deny の Resource を、この鍵に絞る。`modules/kms` の `key_arn_for_iam` を返し、Key Policy の置き換えを待たない。Key Policy が名指す role の作成が、この ARN を Resource に持つ apply-identity の inline policy の書き込みの後に来るため（[deployment-identity/ADR-0201](../deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定4）。鍵を使う resource には渡さない |
| `state_key_alias_name` | bootstrap の手順が鍵を名指す名前（`alias/<project>-<env>-tfstate`）。手順と `env/admin/identity/tests/imports.tftest.hcl` と `scripts/tf-local`（`state-key` が alias の一覧からこの名前で鍵の ARN を引く）がそれぞれ同じ値を写し、テストはこの出力を `imports.tftest.hcl` の直書きで固定し、`scripts/tf-local` のテストは自分の写しをその直書きと突き合わせる（手順との一致は検査しない）。`modules/kms` の `alias_name` を返し、入力だけから決まるので、鍵を作る前から参照できる |
| `plan_output_bucket_name` | CI の plan の job。plan の出力の保存先 |
| `plan_output_bucket_arn` | [`deployment-identity`](../deployment-identity/README.md)。plan の role の書き込みと、Bucket の設定の Resource を絞る |

- ログの所在: まだ無い。S3 サーバーアクセスログの CloudWatch Logs への配送は段5 で足し、その Log Group を出力に加える
- 基盤系アラームの通知先 Topic ARN: 受けない。フェーズ0 の `state-backend` は対象外である（[scope.md 第7節](../../docs/v1.0-plan/scope.md)）

## 段ごとに持つもの

| 段 | 持つもの | 状態 |
| --- | --- | --- |
| 段1・段2 | Bucket、Public Access Block、`BucketOwnerEnforced`、版管理。`env/admin/identity/` の `import` block。既定の暗号化は取り込まず、段4 の apply までは AWS の既定（SSE-S3 と SSE-C の拒否）が掛かる | 本ディレクトリと [`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) |
| 段3-4 | TLS を要求し、object の書き込みを CI の role と key と操作の組に限り、state の鍵以外の暗号化を拒否する Bucket Policy（[state-backend/ADR-0104](docs/adr/0104-state-object-write-deny.md)）。新しいアカウントでは段4 の実行者を `bootstrap_executor_user_name` で例外に入れる | `modules/s3`。state Bucket に入るのは段4 の apply で、その Pull Request の merge の前に、CI の plan（`terraform-gates.yaml` の `plan-admin-identity`）が変更なしを示す |
| 段4 | plan の出力の Bucket（state Bucket と同じ不変条件と、lifecycle） | `internal/plan-output`。段1 の CLI では作らず、段2 の plan に追加として現れる |
| 段4 | state Bucket の lifecycle（上書きされた版を保持日数で消す） | `modules/s3`。段1 の CLI では作らず、段2 の plan に追加として現れる |
| 段4 | state の鍵（CMK、Key Policy、alias） | `modules/kms`。段1 の CLI では作らず、段2 の plan に追加として現れる |
| 段4 | 2つの Bucket の既定の暗号化（state の鍵による SSE-KMS、Bucket Key を使わない、SSE-C の拒否）。backend は init で鍵の ARN を渡す | `modules/s3` と `internal/plan-output`。state Bucket の分は段2 の plan に追加として現れる（[state-backend/ADR-0101](docs/adr/0101-sse-kms-with-state-key.md)）。鍵ができる前の段2-4 の init は鍵を渡さず、state は SSE-S3 で書かれる。段5 で state を書き換える最初の CI の apply が SSE-KMS で書き直す |
| 段5 | 段4 の実行者の書き込みの例外を外す（`bootstrap_executor_user_name` を `null` に戻す）。2つの Bucket の S3 サーバーアクセスログの CloudWatch Logs への配送 | 例外の除去は CI の apply（`terraform-apply.yaml`）が apply する。ログの配送は未実装（#19。#108 の後） |

lock は S3 backend の lock file（`<key>.tflock`）で取る（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定8）。DynamoDB の table を持たない。CI の apply が途中で止まって残った lock は、CI の lock の解除の job が外す（[`.github/workflows/README.md`](../../.github/workflows/README.md) の「lock の解除の job」）。書き込みの拒否が入った後は、lock file を消せる主体は CI の apply の role と lock の解除の role だけである。

## supported / unsupported

supported:

- 1つのアカウントに1つの state Bucket を置き、identity root と workload root の state をそれぞれ1つの key で置く構成（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定2・7）
- 1つのアカウントに1つの plan の出力の Bucket を置き、そのアカウントの root の CI の plan の全文を保持日数のあいだ残す構成

unsupported:

- 管理アカウントの root による bucket policy の削除を防ぐこと。root の封印は組織の運用とする（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定11、[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定19）
- bucket policy で締め出したときに回復する資格情報の発行と管理（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定9）
- 段5 の後のアカウントで、段4 の手順をそのまま回復に使うこと。state と lock file は state の鍵で暗号化され、Key Policy に名指されない主体（一時 IAM user を含む）は state を読めない。回復には Key Policy が名指す主体の資格情報が要り、その発行と管理は責務外とする（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定9、[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定19、[state-backend/ADR-0101](docs/adr/0101-sse-kms-with-state-key.md)）
- state Bucket のリージョン間の複製（リージョン障害への state の備え）。Checkov の CKV_AWS_144 は、この判断を理由に `modules/s3/main.tf` で抑止している
- plan の出力を保持日数より長く残すこと
- 保持日数を過ぎた state の版へ戻すこと（[state-backend/ADR-0103](docs/adr/0103-expire-noncurrent-state-versions.md)）

未決:

- 1つのアカウントに複数の env を置く構成。サポートするかを決めるときに、root と key をあわせて決める（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定1）

## 所有者

- state Bucket はこのユースケースが所有する。同じアカウントの identity root と workload root が、backend として共有する
- plan の出力の Bucket もこのユースケースが所有する。書くのは `deployment-identity` の plan の role だけである
- state の鍵（CMK）も、このユースケースが所有する

## 依存するユースケースと判断 Issue

- ユースケース: `deployment-identity` の `plan_role_arn`・`apply_role_arn`・`apply_identity_role_arn`・`unlock_identity_role_arn` を `ci_role_arns` に受ける。`deployment-identity` はこのユースケースの `bucket_arn`・`plan_output_bucket_arn`・`state_key_arn` を受ける。どちらの向きも resource の ARN を渡すだけで、role と Bucket は互いに依存しない
- 判断 Issue: #130（管理アカウントの作成前提）、#96（state Bucket のアカウント配置）、#98（常駐層の完了条件）、#93（層検査の入力形式と道具）、#83（state の分割粒度）。いずれも閉じている
- plan の role は plan の出力を書くのに鍵の `kms:GenerateDataKey` を要り、それを IAM ではなく Key Policy が許すので、Key Policy に名指す。apply の role は workload root の state を暗号化するときに要るので、workload root を足す変更が Key Policy に足す
- 段5 は log-group（#108）の一般 module を要る。段5 で要る apply-identity の権限は、段5 の変更が先に `deployment-identity` へ足す。鍵と alias の分は足してあり、ログの分は #108 の後に足す。CI の role は鍵と alias を作れないので、鍵の作り直しは CI では行えない（[deployment-identity/ADR-0201](../deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定3）

## 検証方法

- Unit Test（mock の plan）: `tests/naming.tftest.hcl` が名前の組み立てと入力の検査を、`tests/contract.tftest.hcl` が公開 output と `state_noncurrent_version_retention_days`・`plan_output_retention_days`・`ci_role_arns`・`bootstrap_executor_user_name` の検査と配線（Bucket Policy の書き込みの例外へ届く role と key と操作を含む）を、`reference/tests/plan.tftest.hcl` と `env/admin/identity/tests/imports.tftest.hcl` が reference と管理アカウントの root の Bucket 名と alias を確かめる。Bucket の属性の値（既定の暗号化の鍵と Bucket Key を含む）と lifecycle の規則は `modules/s3/tests/` と `internal/plan-output/tests/` が、鍵の属性と Key Policy の文書は `modules/kms/tests/` が見る。2つの Bucket の既定の暗号化が state の鍵を名指すことは `tests/contract.tftest.hcl` が、backend に渡す鍵の ARN の組み立ては `scripts/tf-local` の `state-key` のテストが見る
- テストが見ないもの: `ci_role_arns` の `plan` と `apply_identity` が `modules/kms` の `users` に渡ること、管理アカウントの root で `ci_role_arns` に渡る role が役割どおりであること（テストは子 module の入力を観測できない）。CI の plan が示す `aws_kms_key_policy` の文書の `RolesViaS3ReadWrite` の Principal と、merge の後の `get-key-policy` を、人が確かめる。`unlock_identity` の文の Principal と、復号を限る lock file の object が IAM の削除の Resource と一致することは、Policy Test（`plan.deployment_identity_unlock_role`）が reference の plan で見る。lock file の key は、このユースケースの `main.tf`（Key Policy と Bucket Policy）、`deployment-identity` の IAM、`conftest/plan/decl.json`（`unlock_role.lock_key`）、`env/admin/identity/backend.tf` がそれぞれ写す。2つの root の state の key は、このユースケースの `main.tf`（Bucket Policy）、`deployment-identity` の IAM、`conftest/plan/decl.json`（`state_backend.roots`）、各 root の `backend.tf` がそれぞれ写す。IAM の object が state の Bucket の ARN と宣言の key から成ることと、Key Policy の復号の object が IAM の object と同じことは、同じ Policy Test が見る。`backend.tf` の key と宣言の key の一致は、`scripts/tf-local` のテスト（`Test_lockKey`）が見る
- 段2 の突合: 人が実 AWS で `terraform plan` を実行し、取り込んだ resource に変更と破棄が0件で、追加が段1 で作らないものだけであることを確かめる（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定8）。`import` block が `modules/s3` の resource のうち段1 の CLI が作るもの（既定の暗号化を除く）をすべて覆っていること、その id が正しいことを見るのはこの plan だけで、自動の検査は無い
- 実環境テストの形: [ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14 の「常駐する実体を持つが、同じ構成の複製を作れる」に当たる。実行ごとに sandbox へ複製を作り、apply→検証→destroy して、期待残存との照合で余剰0件を確かめる。複製は常駐する Bucket と backend・名前・鍵を共有しない

sandbox のアカウント（`organization-baseline`、フェーズ1）が無い間は、複製のテストを実行しない。その間に検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

- account regional namespace の Bucket の作成が、実 API で通らないこと
- 実 provider が `import` block の id を受け付けないこと、取り込んだ後の read が宣言と異なる値を返すこと（人が行う段2 の plan だけが見る）
- `force_destroy = false` の Bucket が、版の付いた object と delete marker を残したまま destroy に失敗すること
- TLS でない要求が、実 API で Bucket Policy に拒否されること
- 新しく作るときに、Bucket Policy の作成が Public Access Block の設定と競合して失敗すること
- 実 API が読み戻した Bucket Policy が宣言と違い、plan が毎回差分を出すこと（管理アカウントでは、段4 の apply の後の plan が見る）
- 2つの Bucket の lifecycle が、実 API で日数どおりに版と delete marker を消すこと（S3 は失効を非同期に行う）
- 作成直後の role を名指す Key Policy の書き込みが、IAM の結果整合で失敗すること（新しいアカウントの段4 と、CI の role を足す apply。Provider は retry する）
- `unlock_identity` の暗号化コンテキストで限った復号が、実 API の lock file の読み取りで通ること。lock file が在るときしか評価されない（人が `docs/setup/bootstrap.md` 段5 の lock の項で一度試す）
- 鍵の destroy の後も、削除の猶予（30日）のあいだ鍵が残ること
- Bucket Policy の書き込みの拒否が、実 API で `aws:PrincipalArn` の例外どおりに評価されること（例外の role が通ることは書き込みの拒否を入れる apply とその後の plan が、例外に無い主体が拒まれることは人が `docs/setup/bootstrap.md` 段5 の `put-object` で一度だけ見る）
- multipart の部分の upload と完了の要求で、S3 が暗号化の条件キー（`s3:x-amz-server-side-encryption-aws-kms-key-id`）を埋めるか、どの形で埋めるか。鍵 ID の形で埋めるなら、state が multipart で書かれる大きさになったとき書き込みが拒まれる。backend（Terraform v1.16.6）は Uploader の part の大きさを指定せず、SDK の既定に従う
- 新しいアカウントの段4 の apply の途中で既定の暗号化が SSE-KMS に変わっても、鍵を渡さない backend の state と lock file の書き込みが SSE-S3 で通ること
- SSE-KMS に切り替えた後も、切り替えの前に SSE-S3 で書かれた object を、CI の role がそのまま読めること（管理アカウントでは、切り替えの apply の後の plan が見る）
- init が鍵の ARN を渡す backend の書き込みが、Bucket の既定と同じ鍵で暗号化されること（管理アカウントでは、人が `docs/setup/bootstrap.md` 段5 の `head-object` で見る）

Policy Test（`make tf-policy`）は `reference/tests/policy-input.tftest.hcl` の plan に対して、2つの Bucket の Bucket Policy の Allow の Action の wildcard の不在と、state の鍵の Key Policy の規則（`plan.kms_key_policy`）と、`unlock_identity` への復号が lock file の object に限られること（`plan.deployment_identity_unlock_role`。`deployment-identity` の reference の plan と結合して読む）と、state Bucket の書き込みの拒否と暗号化の拒否の形と、state の鍵の暗号操作を許す role の集合（`plan.state_backend_write_deny`。同じく結合して読む）を確かめる。CI の plan と apply は、実際の plan に `plan.stateful_protection` を掛け、2つの Bucket と lifecycle の削除と置換、lifecycle が版を消す日数の短縮を、許可の宣言なしには落とす（[state-backend/ADR-0103](docs/adr/0103-expire-noncurrent-state-versions.md) 決定7）。object lock と `mfa` の不在を見る Policy は無い（#210）。層検査（#74）は未配線である。配線された後に遡って検証する。

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-10-25（lifecycle と backup の項は 2026-11-08、state の暗号化の項は 2026-11-12）

- AWS / HashiCorp が publisher の state backend の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `tfstate` / `backend` / `state-backend` / `s3-backend`、`provider=aws` で引いた結果に、namespace `aws-ia`・`hashicorp`・`aws`・`aws-samples` が無い）
- 参照した一次資料: [S3 backend](https://developer.hashicorp.com/terraform/language/backend/s3)、[import block](https://developer.hashicorp.com/terraform/language/block/import)、AWS Provider 6.70.0 の `aws_s3_bucket` 系の文書と schema、AWS CLI 2.36.40 の `s3api` reference、[AWS Prescriptive Guidance の Backend best practices](https://docs.aws.amazon.com/prescriptive-guidance/latest/terraform-aws-provider-best-practices/backend.html)、[Security Hub の S3 の control](https://docs.aws.amazon.com/securityhub/latest/userguide/s3-controls.html)（S3.10）、[S3 Lifecycle の設定例](https://docs.aws.amazon.com/AmazonS3/latest/userguide/lifecycle-configuration-examples.html)。state の暗号化の項は、Terraform v1.16.6 の S3 backend の source（`internal/backend/remote-state/s3/client.go`・`backend.go`・`validate.go`。`encrypt` と `kms_key_id` の扱い）と、Amazon S3 User Guide（[Using SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html)、[Specifying SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/specifying-kms-encryption.html)、[Setting default encryption](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-encryption.html)、[S3 Bucket Keys](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-key.html)）、Amazon S3 API Reference の [PutBucketEncryption](https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutBucketEncryption.html)

採用した知見:

- lock を S3 backend の lock file で取る（DynamoDB による lock は deprecated）
- `import` block で取り込み、`terraform import` コマンドを使わない（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定6）
- backend の `bucket` を部分構成で渡す。名前にアカウント ID が入るので、派生したリポジトリが backend の block を書き換えずに済む
- 版管理を持つ Bucket に lifecycle を置き、上書きされた版を保持日数で消す（Security Hub S3.10。[state-backend/ADR-0103](docs/adr/0103-expire-noncurrent-state-versions.md)）
- backend は `encrypt = true` のまま `kms_key_id` に鍵の ARN を部分構成で渡す。`encrypt` が真で `kms_key_id` が無い backend は state と lock file の書き込みに SSE-S3 を明示し、S3 は要求の header を Bucket の既定より優先する。鍵は alias ではなく ARN で名指す（S3 の Setting default encryption）（[state-backend/ADR-0101](docs/adr/0101-sse-kms-with-state-key.md)）

採用しなかった知見と理由:

- DynamoDB の lock table: deprecated であり、[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定8 が lock file を採る
- MFA delete と Object Lock: [state-backend/ADR-0102](docs/adr/0102-no-mfa-delete-or-object-lock.md)
- AWS Backup による state Bucket の backup（AWS Prescriptive Guidance の Backend best practices が版管理とあわせて推奨する）: AWS Backup は独立したユースケースにせず、関係するユースケースの内部で扱う（[scope.md 第8節](../../docs/v1.0-plan/scope.md)）。このユースケースの回復点は、版管理と保持日数のあいだ上書きされた版を残す lifecycle（[state-backend/ADR-0103](docs/adr/0103-expire-noncurrent-state-versions.md)）が担う。backup を足すかは `backup-restore`（#66）が決める
- resource identity での import: Terraform 1.12 以上を要し、Bucket 名の id で足りる
- backend の `allowed_account_ids`: アカウント ID をリポジトリに書くことになる
- `encrypt = false` で Bucket の既定の暗号化に任せる、`backend.tf` に alias を literal で書く、AWS 管理鍵（`aws/s3`）、S3 Bucket Key: [state-backend/ADR-0101](docs/adr/0101-sse-kms-with-state-key.md) の検討した代替案
- 切り替えの前に SSE-S3 で書かれた state の版を copy-object で書き直す: 実環境への手での変更になる。旧版は lifecycle の日数で消える（[state-backend/ADR-0103](docs/adr/0103-expire-noncurrent-state-versions.md)）

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

記録は use-case ADR が持ち、一般 module に由来する逸脱はその module の `README.md` が持つ（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）。一覧は [`docs/adr/README.md`](docs/adr/README.md)。

- 管理アカウント自身の state Bucket を管理アカウントに置く: [state-backend/ADR-0001](docs/adr/0001-management-account-placement.md)
- 新しいアカウントの段1 から、段5 で state を書き換える最初の CI の apply まで、state を SSE-S3 で書き、段4 の apply まで既定の暗号化を AWS の既定に頼る: [state-backend/ADR-0101](docs/adr/0101-sse-kms-with-state-key.md)
- MFA delete と Object Lock を掛けない: [state-backend/ADR-0102](docs/adr/0102-no-mfa-delete-or-object-lock.md)
- state の鍵の Key Policy が、アカウントへの委任から暗号操作を外す: [`modules/kms`](../../modules/kms/README.md) の「公式推奨からの逸脱」

## 版

- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` と `override_resource` の要求であり、実行して確かめたのは 1.16.6（`mise.toml`）だけである。identity root は `>= 1.15.0`（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の `aws login` の条件）
- AWS Provider は `>= 6.68.0, < 7.0.0`。reference と identity root の lock は 6.70.0 を固定する
