# providers

provider の版の正本である（ADR-0205 決定9）。

| ファイル | 役割 |
| --- | --- |
| `providers.toml` | 各 provider の source と版を1つに固定する宣言。人が書く。版は完全一致（`X.Y.Z`）だけを受ける |
| `.terraform.lock.hcl` | `providers.toml` から `terraform providers lock` が書く生成物。手で編集しない |

root（`env/<x>`・`env/<x>/identity`・`usecases/<x>/reference`）の `.terraform.lock.hcl` は、この lock のバイト単位の写しである。root でない module の init（`make tf-init`）は、`required_providers` が正本と同じ集合を要求するディレクトリへこの lock を写し、すべてのディレクトリを `-lockfile=readonly` で init する。

## 版を上げる

`providers.toml` の `version` を変え、次の1コマンドで正本と root の lock を作り直す。

```sh
make tf-providers-lock
```

lock を書く platform は `mise.toml` の `lockfile_platforms` から導く。

## 保証すること

- root の lock が正本と同じ内容であること（`make tf-lock-check`。`make tf-init` が先に走らせる）
- 走査の root（`usecases`・`modules`・`env`）の下で git が追跡する lock が root のものに限られ、どの root の lock も追跡されていること（同上）
- 正本の lock が `providers.toml` と同じ provider の集合と版を持ち、platform ごとの `h1:` と `zh:` を持つこと（同上）
- root でない module の init が正本の版を使うこと（`scripts/tf-gates` の Go のテスト）
- 正本が固定する版が公開から待機期間（レジストリの窓）を経ていること（`make tool-cooldown-gate`。required の `tool-cooldown`）

## 保証しないこと

- `h1:` が、どの platform の照合値かを照らすこと。数が `lockfile_platforms` と同じことと、値の形だけを見る
- root で人が `terraform init -upgrade` や `terraform providers lock` を走らせた結果を、正本へ戻すこと。ずれは `make tf-lock-check` が落とすので、`make tf-lock-apply` で写し直すか、`make tf-providers-lock` で作り直す
