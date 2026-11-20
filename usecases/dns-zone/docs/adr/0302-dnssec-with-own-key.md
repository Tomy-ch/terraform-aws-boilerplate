# ADR-0302: DNSSEC をこのユースケースの非対称の鍵で有効にし、2つの alarm を置く

- Status: Accepted
- Date: 2026-11-20
- Scope: dns-zone
- Related: repository-wide/ADR-0301, repository-wide/ADR-0207, dns-zone/ADR-0101, dns-zone/ADR-0201, dns-zone/ADR-0303

## Context

AWS は、親の NS を zone の name server と一致させることに加え、DNSSEC を dangling delegation への追加の防御として勧める（Route 53 Developer Guide「Protecting against dangling delegation records」）。Route 53 の KSK は、us-east-1 の `ECC_NIST_P256` の署名鍵を要し、その Key Policy で `dnssec-route53.amazonaws.com` に `DescribeKey`・`GetPublicKey`・`Sign` と、`kms:GrantIsForAWSResource` を条件にした `CreateGrant` を許す（同「Working with customer managed keys for DNSSEC」）。AWS は、署名の故障を検知する alarm を強く勧める（同「Configuring DNSSEC signing」）。その metric は us-east-1 にだけ出る。`modules/kms` は対称の暗号化鍵だけを作る。

## Decision

1. zone の DNSSEC の署名を有効にする。KSK は `ACTIVE`、署名は `SIGNING` とする。
2. KSK の鍵は `internal/dnssec-key` が持つ。us-east-1、`ECC_NIST_P256`・`SIGN_VERIFY`、単一 region、削除の猶予 30 日。非対称の鍵は自動 rotation を持てないので、`enable_key_rotation = false` とし、KSK の rotation は人の手順とする。
3. Key Policy は、アカウントの root への管理の委任（暗号操作と `kms:CreateGrant` を含まない。repository-wide/ADR-0301 (least-privilege-policy) 決定12）と、`dnssec-route53.amazonaws.com` への2文とする。2文とも `aws:SourceAccount` で要求元を限り、`aws:SourceArn` を置かない。zone の ARN を参照すると、zone を作る plan で文書が決まらず、CI の実 plan に掛ける Policy が読めない文書として落とす（`PLAN-KMS-09`）。`arn:aws:route53:::hostedzone/*` の pattern はアカウントを持たず、`aws:SourceAccount` より狭くならない（repository-wide/ADR-0301 (least-privilege-policy) 決定11）。同じアカウントの別の zone を区別しないことは「意図的に捨てるもの」に置く。
4. 2つの metric（`DNSSECInternalFailure`・`DNSSECKeySigningKeysNeedingAction`）に、us-east-1 の alarm を置く。通知先は同じアカウントの us-east-1 の Topic（入力 `notify_topic_arn`）とする。CloudWatch の alarm の action が別のリージョンの Topic を受けるかを確かめていないので、同じリージョンに限る（#421）。
5. 子の zone の DS は、子の zone の署名が有効になった後に prd の `delegations` へ写す。apex の DS は registrar へ人が登録する。DNSSEC を無効にするときは、親の DS を先に外し、DS の TTL を待ってから署名を無効にし、KSK を無効にする（同「Disabling DNSSEC signing」）。

## 検討した代替案

### 案A: DNSSEC を持たない

AWS が追加の防御として勧める構成を持たない。subdomain takeover への防御が、置換を止めることと NS の写しの手順だけになる。

### 案B: `modules/kms` に非対称の鍵を足す

鍵の種別で鍵の構造と Key Policy の形が変わる入力になり、切り替えに当たる（repository-wide/ADR-0102 (use-case-centric-scope) 決定15）。

### 案C: Provider の例のとおり、アカウントの root に `kms:*` を許す

暗号操作を IAM の委任に載せる（repository-wide/ADR-0301 決定12）。

### 案D: alarm を置かない

署名が止まったことと、KSK が鍵を使えなくなったことに気づけない。DS を登録した後は、名前の解決の停止になる。

## 意図的に捨てるもの

- 同じアカウントの別の zone の KSK がこの鍵を使うことを区別すること。
- KSK の自動 rotation。
- us-east-1 の Topic が無い環境への配線（#421 まで env へ配線できない）。

## 保証範囲

- 鍵の種別・region・Key Policy は `internal/dnssec-key/tests/` と PLAN-DNS-03 と `plan.kms_key_policy` が見る。KSK と署名の状態、alarm の metric・region・通知先は `internal/zone/tests/` と PLAN-DNS-04 が見る。
- Key Policy の2文の `aws:SourceAccount` を Route 53 が受けて KSK を作れること、KSK を作る role に鍵の権限が要らないことは確かめていない（#331）。

## 検証方法

- `internal/dnssec-key/tests/`、`internal/zone/tests/`、`conftest/plan/dns_zone.rego`（PLAN-DNS-03・04）、`conftest/plan/kms_key_policy.rego`

## 影響

- 削除待ちの DNSSEC の鍵は、sandbox の複製の destroy の後の期待残存になる（Security Hub KMS.3）。
- Security Hub の KMS.4 が非対称の鍵に Finding を出すなら、README の「公式推奨からの逸脱」の記録が対応する。

## 見直し条件

- CloudWatch の alarm が別のリージョンの Topic を受けると確かめたとき（通知先の region の限定を外す）。
- KMS が非対称の鍵の自動 rotation を持ったとき。
