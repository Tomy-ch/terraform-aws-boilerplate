# dns-zone

各環境のアカウント（dev・stg・prd・sandbox）に、その環境の公開の Route 53 Hosted Zone を1つ持つユースケース。prd の zone は登録したドメインの apex で、dev と stg の zone へ委任する。record は、それが指す先を持つユースケースが書く。常駐層である（[dns-zone/ADR-0001](docs/adr/0001-one-zone-per-environment-account.md)、[scope.md 第7節・第10.2節](../../docs/v1.0-plan/scope.md)）。

## 解決する問題

公開面を持つユースケース（`static-web`、`public-api-alb` など）は、環境ごとの名前を同じ zone に書く。zone の持ち主が record を書くユースケースの1つだと、そのユースケースの destroy で zone が消え、親に残った委任の NS が宙に浮く（dangling delegation。subdomain takeover の経路）。zone を作り直すと name server が変わり、親の委任と食い違う。このユースケースが zone と委任を1つの持ち主として持ち、zone の置換と削除を止め、他のユースケースへは識別子で渡す。

## 実行形態とネットワーク境界

- AWS の上で動く処理は無い。資源は Terraform が作る
- zone は公開の zone だけで、VPC を持たない。private zone（`vpc`）と VPC の関連付けを持たない
- 公開の DNS への応答は Route 53 の権威サーバが行う。zone は DNSSEC で署名する（[dns-zone/ADR-0302](docs/adr/0302-dnssec-with-own-key.md)）
- us-east-1 に置く資源（クエリログの Log Group と鍵、resource policy、DNSSEC の鍵、DNSSEC の alarm）は、Route 53 がその region にしか受けないためである。通信の経路ではない（[dns-zone/ADR-0303](docs/adr/0303-us-east-1-by-region-argument.md)）

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字、42文字以内） | 名前の先頭の token |
| `env` | `dev` / `stg` / `prd` / `sandbox` | zone を置くアカウントの env の token。prd の zone は `apex_domain`、ほかは `<env>.<apex_domain>` |
| `apex_domain` | `string`（小文字の FQDN、末尾のドットを持たない、240文字以内） | 登録したドメインの apex。登録は人の手順（[`docs/setup/dns-zone.md`](../../docs/setup/dns-zone.md)） |
| `delegations` | `map(object({ name_servers = list(string), ds_record = optional(string) }))` | prd の zone から dev・stg の zone へ委任する値。key は `dev` か `stg`。`name_servers` は子の zone の出力 `name_servers`（Route 53 の name server を重ねずに4つ）、`ds_record` は子の zone の出力 `ds_record`（`<key tag> 13 2 <digest>`）。prd のほかは空の map を渡す（渡すと plan で落ちる） |
| `query_log_retention_days` | `number` | 公開 DNS のクエリログを残す日数。365 以上の、CloudWatch Logs が受ける値 |
| `query_log_deletion_protection` | `bool` | クエリログの Log Group の削除を拒むか。sandbox のように destroy する環境では `false` |
| `notify_topic_arn` | `string`（us-east-1 の SNS の Topic の ARN） | DNSSEC の alarm の通知先。同じアカウントの us-east-1 の Topic（#421） |

どの入力も既定値を持たない。

| 出力 | 使う側 |
| --- | --- |
| `zone_id` | record を書くユースケース（`dns = { zone_id, name }`。[scope.md 第3.1節](../../docs/v1.0-plan/scope.md)） |
| `zone_arn` | record を書く role の権限の Resource（`deployment-identity`） |
| `zone_name` | record を書くユースケースと、record を書く権限の名前の絞り込み。末尾のドットを持たない |
| `name_servers` | dev・stg では prd の `delegations`、prd ではドメインの registrar（人が写す） |
| `ds_record` | dev・stg では prd の `delegations`、prd ではドメインの registrar（人が写す） |
| `query_log_group_arn` | ログの所在（us-east-1。末尾の `:*` を持たない） |

- ログの所在: 公開 DNS のクエリログは `query_log_group_arn`（`/aws/route53/<zone の名前>`）。API の呼び出しの記録は CloudTrail が持つ
- 基盤系アラームの通知先 Topic ARN: `notify_topic_arn` で受ける。us-east-1 の Topic を出す変更は #421

## 持つもの

| 構成 | 中身 |
| --- | --- |
| `internal/zone` | 公開の Hosted Zone（`force_destroy = false`、`enable_accelerated_recovery = false`）、クエリログの設定、KSK（`ACTIVE`）と DNSSEC の署名、委任の NS（TTL 3600）と DS（TTL 300）、DNSSEC の2つの alarm（`DNSSECInternalFailure`・`DNSSECKeySigningKeysNeedingAction`、us-east-1） |
| `internal/query-log` | クエリログの鍵（`modules/kms`、alias `<project>-<env>-dns-query-logs`）、Log Group（`modules/log-group`、`/aws/route53/<zone の名前>`）、アカウント単位の resource policy（`<project>-<env>-dns-query-logs`）。すべて us-east-1 |
| `internal/dnssec-key` | KSK の鍵（`ECC_NIST_P256`・`SIGN_VERIFY`、us-east-1、alias `<project>-<env>-dns-dnssec`）と Key Policy |

## まだ持たないもの

| まだ持たないもの | 足す変更 |
| --- | --- |
| zone の control plane の accelerated recovery | prd の zone の変更の可用性を要件にしたとき。型付きの bool として足す（[dns-zone/ADR-0201](docs/adr/0201-zone-replacement-and-deletion-stopped.md)） |
| record set protection（apex の NS・SOA の変更の保護） | AWS Provider が resource を持ったとき |
| Resource ごとの resource policy（`resource_arn`）でのクエリログの許可 | Route 53 がその形を受けることを sandbox で確かめたとき（[dns-zone/ADR-0301](docs/adr/0301-query-logging-in-us-east-1.md)） |

## supported / unsupported

| 区分 | 項目 |
| --- | --- |
| supported | 各環境のアカウントに公開の zone を1つ。prd が apex で、dev と stg へ委任する。DNSSEC の署名とクエリログ |
| unsupported | NS と DS のほかの record をこのユースケースが書くこと。record は指す先を持つユースケースが書く（[dns-zone/ADR-0102](docs/adr/0102-records-written-by-target-owner.md)） |
| unsupported | zone の record をすべて排他に持つこと（`aws_route53_records_exclusive`）。他のユースケースの record を消す |
| unsupported | private zone と VPC の関連付け、Route 53 Resolver |
| unsupported | sandbox の zone の委任。sandbox の zone は公開の DNS から引けない |
| unsupported | 孫の委任（dev・stg の zone のさらに下の zone）と、prd のほかの zone からの委任 |
| unsupported | 別のアカウントの state・provider・role を使った委任の書き込み（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定18）。値は人が env の値へ写す（[dns-zone/ADR-0101](docs/adr/0101-delegation-values-passed-by-input.md)） |
| unsupported | reusable delegation set、ドメインの登録（Route 53 Domains）、health check と failover の record |
| unsupported | DNSSEC を使わない zone、クエリログを持たない zone |
| unsupported | 国際化ドメイン名の TLD（`xn--` で始まる TLD）。`apex_domain` は英小文字の TLD だけを受ける |

## 所有者

| 資源 | 持ち主 |
| --- | --- |
| zone、委任の NS と DS、クエリログの設定と Log Group と鍵、DNSSEC の鍵と KSK と署名、DNSSEC の alarm | このユースケース |
| us-east-1 のアカウント単位の CloudWatch Logs の resource policy の枠（リージョンに10個まで）のうち1つ | このユースケース。同じ region に policy を足すユースケースは、この1つを数に入れる。sandbox で複製を並行して作ると、複製ごとに1つ使う |
| NS と DS のほかの record | その record が指す先を持つユースケース |
| record を書く role の権限（`route53:ChangeResourceRecordSets` の名前の絞り込み） | `deployment-identity`（#330） |
| ドメインの登録、registrar の NS と DS、KSK の rotation | 人（[`docs/setup/dns-zone.md`](../../docs/setup/dns-zone.md)） |
| 通知先の us-east-1 の Topic | `ops-notification`（#421） |

## 依存するユースケースと判断 Issue

- 依存するユースケース: `ops-notification`（us-east-1 の Topic。#421）。それまで env へ配線できない
- 判断: #94（共有基盤の持ち主、Route 53 の論点）、#98（常駐層の完了条件）、#86（sandbox の常駐）

## record を書くユースケースへの契約

- record は `zone_name` の下の名前だけに書く。apex（`zone_name` そのもの）、NS、DS、SOA を書かない
- record を書く role の `route53:ChangeResourceRecordSets` は、`ForAllValues:StringEquals` か `ForAllValues:StringLike` の `route53:ChangeResourceRecordSetsNormalizedRecordNames` で、そのユースケースが書く名前に限る（Route 53 Developer Guide「[Using IAM policy conditions for fine-grained access control](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/specifying-conditions-route53.html)」）。名前は小文字で末尾のドットを持たず、`*` は `\052` と書く。Deny ではなく Allow で絞る。Policy での検査は、その Allow を足す `deployment-identity` の変更が持つ（#330）
- record を、指す先と同じユースケースに置き、指す先と一緒に destroy する

## 検証方法

| 層 | 道具 | 何を見るか |
| --- | --- | --- |
| Unit | `internal/*/tests/`、`tests/composition.tftest.hcl` | zone の名前の導出、`force_destroy` と accelerated recovery の固定、委任の NS と DS の名前・型・TTL、DNSSEC の鍵の種別と region と Key Policy、クエリログの Log Group の名前と resource policy と、保持日数と削除の保護が Log Group に届くこと、alarm の metric と region と通知先、prd のほかの環境が委任を持てないこと（`tests/composition.tftest.hcl`） |
| Contract | `tests/contract.tftest.hcl`、`tests/validation.tftest.hcl` | 出力の配線、入力の許容値 |
| Policy | `conftest/plan/dns_zone.rego` と `conftest/plan/stateful_protection.rego`（入力は `reference/tests/policy-input.tftest.hcl` の plan） | 下の表 |
| Integration / E2E | sandbox の apply の経路（#331）で行う | 下の「保証しないもの」の各項 |

| Policy | 何を見るか |
| --- | --- |
| `PLAN-STATE-*` | zone・KSK・DNSSEC の署名の置換・削除・管理からの除外を、root の許可の宣言が無い限り止める（[dns-zone/ADR-0201](docs/adr/0201-zone-replacement-and-deletion-stopped.md)） |
| `PLAN-DNS-00` | 宣言の形と、宣言した address が宣言どおりの型で入力にあり、alarm と委任の NS・DS の record が宣言した件数であること |
| `PLAN-DNS-01` | zone の `force_destroy` と accelerated recovery が明示の `false` |
| `PLAN-DNS-02` | クエリログが、宣言した鍵で暗号化した保持 365 日以上の Log Group を指し、Log Group・その鍵・resource policy が us-east-1 にあり、resource policy が Route 53 にその Log Group だけを許すこと |
| `PLAN-DNS-03` | DNSSEC の鍵の種別・region・Key Policy と、KSK が宣言した鍵を使うこと |
| `PLAN-DNS-04` | KSK と署名が有効で、DNSSEC の2つの alarm が us-east-1 にあること |
| `PLAN-DNS-05` | このユースケースの record が NS か DS で、名前が zone の apex でなく、zone の名前の下の DNS のラベル（各63字以下）だけでできていて、全体が253字以下で、`*` と `\052` を含まないこと |
| `PLAN-DNS-06` | ユースケースの下に宣言の外の型が無いこと |

**完了条件の区分:** [scope.md 第11.1節](../../docs/v1.0-plan/scope.md)の「複製を作れる」。sandbox で、常駐する zone と名前・鍵・Log Group・resource policy を共有しない複製（別の `project` と委任しない `apex_domain`）を作り、apply → 検証 → destroy する。複製は委任されないので、公開の DNS からの到達ではなく API の状態で検証する。

sandbox の destroy の後の期待残存（識別子・状態・期限）:

- クエリログの鍵（us-east-1、alias `<project>-sandbox-dns-query-logs`、状態は削除待ち、期限は 30 日）
- DNSSEC の鍵（us-east-1、alias `<project>-sandbox-dns-dnssec`、状態は削除待ち、期限は 30 日）

alias は destroy で消えるので、destroy の前に key ID を控えて照合する。残存検出は us-east-1 も走査する（#76）。

## 保証しないもの

- 実際の AWS での作成と破棄。この変更の時点で、このユースケースを apply する経路が無い（dev・stg・prd は #330、sandbox は #331）
- Route 53 が、鍵で暗号化した Log Group と、Resource を1つの Log Group に絞った resource policy と `aws:SourceAccount` の条件を受けて、クエリログを書けること
- DNSSEC の Key Policy の2つの文に `aws:SourceAccount` を置いても、KSK の作成と署名が通ること。KSK を作る role に鍵の権限が要らないこと（AWS の文書は要求を書いていない）
- CloudWatch の alarm が us-east-1 の Topic へ通知できること（#421）
- 帯域外の変更（別の手段で足した record、親の委任の書き換え）が次の plan の差分に出ること。このユースケースが持たない record は plan に出ない
- prd の `delegations` が、子の zone の実際の name server と DS と一致すること。別のアカウントの値で、plan に出ない。zone の置換を止めることで食い違う契機を塞ぐが、許可の宣言を通した置換の後の写し直しは手順が持つ
- registrar の NS と DS が prd の zone と一致すること、KSK の rotation、DS の不整合からの復旧（人の手順）
- クエリログがすべての DNS の問い合わせを記録すること。resolver の cache が答えた問い合わせは Route 53 に届かない
- Policy が掛かるのは reference の plan だけである。env の plan に掛けるかは env への配線（#330）で決める
- `aws_route53_zone` の `name` の変更が置換になること（Provider の挙動）。置換になれば `PLAN-STATE-*` が止める

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-20

- AWS が publisher の Route 53 の Hosted Zone の Terraform module は Terraform Registry に無い（AWS と HashiCorp が publisher の Route 53 の module は Application Recovery Controller の `aws-ia/route53-recovery-controller` だけ）
- AWS Provider v6.73.0 の `aws_route53_zone`・`aws_route53_query_log`・`aws_route53_key_signing_key`・`aws_route53_hosted_zone_dnssec`・`aws_route53_record`・`aws_cloudwatch_log_resource_policy` の文書
- Route 53 Developer Guide（[Protecting against dangling delegation records](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/protection-from-dangling-dns.html)、[Using IAM policy conditions](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/specifying-conditions-route53.html)、[Public DNS query logging](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/query-logs.html)、[Configuring DNSSEC signing](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-configuring-dnssec.html)、[Enabling DNSSEC signing](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-configuring-dnssec-enable-signing.html)、[Disabling DNSSEC signing](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-configuring-dnssec-disable.html)、[Working with customer managed keys for DNSSEC](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-configuring-dnssec-cmk-requirements.html)、[Identity and access management](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/access-control-managing-permissions.html)、[Best practices for Amazon Route 53 DNS](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/best-practices-dns.html)、[Accelerated recovery](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/accelerated-recovery.html)）
- AWS Security Hub の Route 53 のコントロール（[Route53.1・Route53.2](https://docs.aws.amazon.com/securityhub/latest/userguide/route53-controls.html)）と KMS のコントロール（[KMS.3・KMS.4](https://docs.aws.amazon.com/securityhub/latest/userguide/kms-controls.html)）
- CIS AWS Foundations Benchmark（Security Hub の v5.0.0・v3.0.0・v1.4.0・v1.2.0）。Route 53 の項目は無い
- AWS Well-Architected と AWS SRA の Route 53 に固有の項目は確認していない

採用した知見:

- 公開の zone のクエリログ（Route53.2）。Log Group は us-east-1 に置き、resource policy は Route 53 にその Log Group だけを許す
- 委任の NS を子の zone の name server と一致させ、子の zone を作る前に親へ NS を書かない（dangling delegation）。zone の置換を止める
- DNSSEC を dangling delegation への追加の防御として有効にし、2つの metric に alarm を置く。DS の TTL を短く始める。無効にするときは DS を先に外す
- 委任は直近の親からだけ行い、委任の NS の TTL を長めにする（Best practices）
- record を書く権限を Normalized record names の条件で絞り、Allow で書く。zone の操作を record の書き手に与えない

採用しなかった知見と理由:

- Route53.1（health check の tag）: health check を持たない
- reusable delegation set: zone の置換を止める方が宙づりの窓を作らない（[dns-zone/ADR-0101](docs/adr/0101-delegation-values-passed-by-input.md)）
- accelerated recovery: 可用性の要件に属し、有効な間は zone を削除できない（[dns-zone/ADR-0201](docs/adr/0201-zone-replacement-and-deletion-stopped.md)）
- record set protection: AWS Provider に resource が無い
- Provider の例の、Key Policy でアカウントの root に `kms:*` を許す形: 暗号操作を IAM の委任に載せない（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定12）

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

- **KMS.4（鍵の自動 rotation）が DNSSEC の鍵に Finding を出す場合。** 非対称の鍵は自動 rotation を持てない。外れる推奨は KMS.4、理由は鍵の種別が Route 53 の要件（`ECC_NIST_P256`）で決まること、影響は鍵の rotation を人の手順（KSK の rotation）が持つこと、代替案は無い、見直し条件は KMS が非対称の鍵の自動 rotation を持ったとき。Security Hub の文書は非対称の鍵の扱いを書いておらず、Finding が出るかは確かめていない
- **KMS.3（削除予定の鍵）が sandbox の複製の destroy の後に Finding を出す。** 期待残存の鍵である（上の「検証方法」）

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。resource の `region` 引数（AWS Provider v6）を使う。`terraform test` と reference の lock は v6.73.0
