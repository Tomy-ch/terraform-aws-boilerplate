# state-backend

Terraform の state を置く S3 Bucket を、1つのアカウントに1つ持つユースケース。bootstrap の構成物であり（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）、各 env の identity root から呼ばれる（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定3・5）。

## 解決する問題

state は、そのアカウントを変更できる主体だけが書ける場所に、版を残して置かなければならない。state backend は Terraform より先に存在する必要があるので、bootstrap の段1 で AWS CLI が作り、段2 で Terraform が取り込む（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定4-8）。このユースケースは、取り込んだ後の Bucket を Terraform の Desired State に置き、以後の変更を CI の apply-identity の role に限る。

## 実行形態とネットワーク境界

- Terraform の S3 backend が、CI のランナーと bootstrap の実行者の手元から、AWS の公開 API の endpoint で読み書きする。AWS の上で動く処理は無い
- 外への egress は無い。Bucket は Public Access Block の4項目をすべて有効にし、ACL を無効にする（`BucketOwnerEnforced`）
- TLS でない要求は、Bucket Policy の Deny で拒否する

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字） | 名前の先頭に置くプロジェクトの token |
| `env` | `dev` / `stg` / `prd` / `sandbox` / `admin` | Bucket を置くアカウントの env の token（[scope.md 第3節](../../docs/v1.0-plan/scope.md)） |

Bucket の完全な名前は `<project>-<env>-tfstate-<account_id>-<region>-an` で、63文字を超えると plan で落ちる。`env = sandbox`・region が `ap-northeast-1` のとき、`project` は16文字までになる。長さの超過と、S3 が予約している接頭辞（`project = "sthree"` のとき `sthree-`）は、`modules/s3` が内部の入力 `name` の誤りとして plan で落とす。

| 出力 | 使う側 |
| --- | --- |
| `bucket_name` | identity root と workload root の backend の `bucket`、段2 の `import` block の id |
| `bucket_arn` | `deployment-identity`（#20）。CI の role の IAM で、S3 の Action の Resource を state Bucket に絞る |

- ログの所在: まだ無い。S3 サーバーアクセスログの CloudWatch Logs への配送は段5 で足し、その Log Group を出力に加える
- 基盤系アラームの通知先 Topic ARN: 受けない。フェーズ0 の `state-backend` は対象外である（[scope.md 第7節](../../docs/v1.0-plan/scope.md)）

## 段ごとに持つもの

| 段 | 持つもの | 状態 |
| --- | --- | --- |
| 段1・段2 | Bucket、Public Access Block、`BucketOwnerEnforced`、版管理、SSE-S3 と SSE-C の拒否。`env/admin/identity/` の `import` block | 本ディレクトリと [`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) |
| 段3-4 | TLS を要求する Bucket Policy | `modules/s3`。state Bucket に入るのは #20 の段4 の apply |
| 段5 | CMK と SSE-KMS への切り替え、root ごとの key と lock file の書き込みの拒否（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定9・10）、lifecycle、S3 サーバーアクセスログの CloudWatch Logs への配送 | 未実装（#19。#20・#107・#108 の後） |

lock は S3 backend の lock file（`<key>.tflock`）で取る（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定8）。DynamoDB の table を持たない。

## supported / unsupported

supported:

- 1つのアカウントに1つの state Bucket を置き、identity root と workload root の state をそれぞれ1つの key で置く構成（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定2・7）

unsupported:

- 管理アカウントの root による bucket policy の削除を防ぐこと。root の封印は組織の運用とする（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定11、[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定18）
- bucket policy で締め出したときに回復する資格情報の発行と管理（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定9）

未決:

- 1つのアカウントに複数の env を置く構成。サポートするかを決めるときに、root と key をあわせて決める（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定1）

## 所有者

- state Bucket はこのユースケースが所有する。同じアカウントの identity root と workload root が、backend として共有する
- 段5 で作る CMK も、このユースケースが所有する

## 依存するユースケースと判断 Issue

- ユースケース: 無い。`deployment-identity`（#20）がこのユースケースの `bucket_arn` を受ける
- 判断 Issue: #130（管理アカウントの作成前提）、#96（state Bucket のアカウント配置）、#98（常駐層の完了条件）、#93（層検査の入力形式と道具）、#83（state の分割粒度）。いずれも閉じている
- 段5 は `deployment-identity`（#20）の apply-identity の role と、kms（#107）・log-group（#108）の一般 module を要る

## 検証方法

- Unit Test（mock の plan）: `tests/naming.tftest.hcl` が名前の組み立てと入力の検査を、`tests/contract.tftest.hcl` が公開 output を、`reference/tests/plan.tftest.hcl` と `env/admin/identity/tests/imports.tftest.hcl` が reference と管理アカウントの root の Bucket 名を確かめる。Bucket の属性の値は `modules/s3/tests/` が見る
- 段2 の突合: 人が実 AWS で `terraform plan` を実行し、取り込んだ resource に変更と破棄が0件で、追加が段1 で作らないものだけであることを確かめる（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定8）。`import` block が `modules/s3` の resource をすべて覆っていること、その id が正しいことを見るのはこの plan だけで、自動の検査は無い
- 実環境テストの形: [ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14 の「常駐する実体を持つが、同じ構成の複製を作れる」に当たる。実行ごとに sandbox へ複製を作り、apply→検証→destroy して、期待残存との照合で余剰0件を確かめる。複製は常駐する Bucket と backend・名前・鍵を共有しない

sandbox のアカウント（`organization-baseline`、フェーズ1）が無い間は、複製のテストを実行しない。その間に検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

- account regional namespace の Bucket の作成が、実 API で通らないこと
- 実 provider が `import` block の id を受け付けないこと、取り込んだ後の read が宣言と異なる値を返すこと（人が行う段2 の plan だけが見る）
- `force_destroy = false` の Bucket が、版の付いた object と delete marker を残したまま destroy に失敗すること
- TLS でない要求が、実 API で Bucket Policy に拒否されること
- 新しく作るときに、Bucket Policy の作成が Public Access Block の設定と競合して失敗すること
- 実 API が読み戻した Bucket Policy が宣言と違い、plan が毎回差分を出すこと（管理アカウントでは、#20 の段4 の apply の後の plan が見る）

Policy Test（#75）と層検査（#74）は未配線である。配線された後に遡って検証する。

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-10-25

- AWS / HashiCorp が publisher の state backend の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `tfstate` / `backend` / `state-backend` / `s3-backend`、`provider=aws` で引いた結果に、namespace `aws-ia`・`hashicorp`・`aws`・`aws-samples` が無い）
- 参照した一次資料: [S3 backend](https://developer.hashicorp.com/terraform/language/backend/s3)、[import block](https://developer.hashicorp.com/terraform/language/block/import)、AWS Provider 6.70.0 の `aws_s3_bucket` 系の文書と schema、AWS CLI 2.36.40 の `s3api` reference

採用した知見:

- lock を S3 backend の lock file で取る（DynamoDB による lock は deprecated）
- `import` block で取り込み、`terraform import` コマンドを使わない（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定6）
- backend の `bucket` を部分構成で渡す。名前にアカウント ID が入るので、派生したリポジトリが backend の block を書き換えずに済む

採用しなかった知見と理由:

- DynamoDB の lock table: deprecated であり、[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定8 が lock file を採る
- MFA delete と Object Lock: [state-backend/ADR-0102](docs/adr/0102-no-mfa-delete-or-object-lock.md)
- resource identity での import: Terraform 1.12 以上を要し、Bucket 名の id で足りる
- backend の `allowed_account_ids`: アカウント ID をリポジトリに書くことになる

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

記録は use-case ADR が持つ。一覧は [`docs/adr/README.md`](docs/adr/README.md)。

- 管理アカウント自身の state Bucket を管理アカウントに置く: [state-backend/ADR-0001](docs/adr/0001-management-account-placement.md)
- 段5 まで SSE-S3: [state-backend/ADR-0101](docs/adr/0101-sse-s3-until-cmk.md)
- MFA delete と Object Lock を掛けない: [state-backend/ADR-0102](docs/adr/0102-no-mfa-delete-or-object-lock.md)

## 版

- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` と `override_resource` の要求であり、実行して確かめたのは 1.16.2（`mise.toml`）だけである。identity root は `>= 1.15.0`（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の `aws login` の条件）
- AWS Provider は `>= 6.68.0, < 7.0.0`。reference と identity root の lock は 6.70.0 を固定する
