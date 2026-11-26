## dns-zone の手順書（docs/setup/dns-zone.md）の、人が実行する確認と手順の実行
## （.makefiles/README.md の `setup/`、ADR-0702 決定26・27）。zone を置くアカウントの profile で行う。
## 端末からの入力を読むので、ホストで起動する（ADR-0504 決定1・3）。

SETUP_DNS_USECASE = usecases/dns-zone

# PROFILE・ENV・ZONE は人が渡す値（.makefiles/README.md の `setup/`）。region は ENV の workload root（env/<ENV>）の provider の region。
setup-dns-zone-%: export SETUP_PROFILE = $(call literal_value,PROFILE)
setup-dns-zone-%: export SETUP_ENV = $(call literal_value,ENV)
setup-dns-zone-%: export SETUP_ZONE = $(call literal_value,ZONE)

# $(1)=入口、$(2)=入口に固有の引数
define SETUP_DNS_RUN
@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/$$SETUP_ENV")); env $$unset_env AWS_PROFILE="$$SETUP_PROFILE" AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,setup) $(1) -project $(TF_LOCAL_PROJECT) -region "$$region" -usecase "$(SETUP_DNS_USECASE)" -env "$$SETUP_ENV" -zone "$$SETUP_ZONE"$(2)
endef

.PHONY: setup-dns-zone-delegation-values ## 3.2: 子の zone の NS と DS を、prd の delegations に書く形で出す（PROFILE=<そのアカウントの profile> ENV=<dev か stg> ZONE=<zone の名前>、DS に12桁続く数字が在れば IDS=1）
setup-dns-zone-delegation-values:
	$(call SETUP_DNS_RUN,dns-delegation-values,$(if $(filter 1,$(IDS)), -ids))

.PHONY: setup-dns-zone-signing-check ## 3.5・4 の確認: 署名が SIGNING で、KSK がただ1つ ACTIVE で、その KMS の鍵が要件に合う（PROFILE=<profile> ENV=<env> ZONE=<zone>）
setup-dns-zone-signing-check:
	$(call SETUP_DNS_RUN,dns-signing-check)

.PHONY: setup-dns-zone-ksk-activate ## 4 手順3: ACTION_NEEDED の KSK を、KMS の鍵が要件に合うときだけ有効に戻す（PROFILE=<profile> ENV=<env> ZONE=<zone>）
setup-dns-zone-ksk-activate:
	$(call SETUP_DNS_RUN,dns-ksk-activate)

.PHONY: setup-dns-zone-registrar-ns ## 3.4: Route 53 Domains の registrar の NS を zone の NS に替える（PROFILE=<prd の profile> ENV=prd ZONE=<apex_domain>）
setup-dns-zone-registrar-ns:
	$(call SETUP_DNS_RUN,dns-registrar-ns)

.PHONY: setup-dns-zone-registrar-ds ## 3.4: registrar の NS の切り替えが行き渡った後に、Route 53 Domains の registrar に KSK の DS を足す（PROFILE=<prd の profile> ENV=prd ZONE=<apex_domain>）
setup-dns-zone-registrar-ds:
	$(call SETUP_DNS_RUN,dns-registrar-ds)
