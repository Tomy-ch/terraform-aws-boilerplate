# Evidence collection — npm（pnpm）

Read this together with the axis definitions in `SKILL.md`. The npm side of this repository is the
`node_tools` image: [`docker/tools/node/`](../../../../docker/tools/node/) holds a `package.json`
whose versions are copies of `mise.toml`, and a `pnpm-lock.yaml` that pins every transitive
dependency by version and integrity. Two windows meet here, and which one caught the candidate
decides what the triage is for:

| Caught by | What it covers | What a triage feeds |
| --- | --- | --- |
| `make tool-cooldown-gate` | the direct dependencies, declared as `npm:` keys in `mise.toml` | the `reason` of a dated `.github/tool-cooldown-bypass.toml` entry |
| pnpm's `minimumReleaseAge`（`make node-tools-lock` が失敗する） | the transitive dependencies resolved into the lockfile | nothing to override —— see the wall below |

**The transitive window has no per-version exemption in this repository.**
`minimumReleaseAgeExclude` is written by `make versions-apply` from the direct dependencies and
checked by `make versions-check`, so a hand-added entry fails the gate. A LOW score on a transitive
candidate means "safe to take once it ages", not "take it now".

Never install the candidate: `pnpm install` / `npm install` run lifecycle scripts, and running an
install script *is* the npm attack. Read the registry metadata and the tarball instead.

Set up:

```sh
PKG=<package>          # e.g. markdownlint-cli2, or @scope/name
CAND=<candidate>       # e.g. 0.24.1
BASE=<baseline>        # the version in docker/tools/node/pnpm-lock.yaml
WORK="$SCRATCH/npm-$(echo "$PKG" | tr '/@' '--')"; mkdir -p "$WORK"; cd "$WORK"
curl -fsSL "https://registry.npmjs.org/$PKG" > meta.json
jq -r --arg c "$CAND" --arg b "$BASE" '.time[$c], .time[$b]' meta.json
```

## Axis P — publisher

```sh
jq -r --arg v "$CAND" '.versions[$v]._npmUser.name' meta.json
jq -r '[.versions[]._npmUser.name] | group_by(.) | map({(.[0]): length}) | add' meta.json
jq -r '.maintainers[].name' meta.json
```

- A `_npmUser` on the candidate that never published an earlier version is the finding.
- A maintainer added shortly before the candidate, or a package dormant for a long time that
  publishes twice in a day, is a `2` until explained.

## Axis A — artifact versus source

```sh
jq '.versions[env.CAND].dist | {integrity, attestations}' --arg CAND "$CAND" meta.json
# 404 は「attestation を持たない」の意味である（取得の失敗ではない）
if curl -fsS "https://registry.npmjs.org/-/npm/v1/attestations/$PKG@$CAND" -o att.json; then
  jq '.attestations[].predicateType' att.json
else
  echo "no attestations"
fi
jq -r --arg v "$CAND" '.versions[$v].gitHead // "none"' meta.json
```

- A SLSA provenance attestation names the source repository, commit and workflow that built the
  tarball. Confirm the commit is on the upstream default branch (`gh api repos/<owner>/<repo>/compare/<default>...<sha>`).
- No attestation and no `gitHead` leaves only the tarball itself: compare it with the tagged
  source (below). If neither is possible, the axis is `?`.
- The lockfile's `integrity` proves the bytes did not change after the lock was written; it says
  nothing about how they were produced.

## Axis D — what actually changed

```sh
for v in "$BASE" "$CAND"; do
  url=$(jq -r --arg v "$v" '.versions[$v].dist.tarball' meta.json)
  mkdir -p "$v" && curl -fsSL "$url" | tar -xz -C "$v"
done
diff -ruN "$BASE/package" "$CAND/package" > range.diff; wc -l range.diff
grep -nE '^\+.*(process\.env|child_process|execSync|spawn\(|eval\(|new Function|atob\(|Buffer\.from\([^)]*base64|https?://[0-9]+\.|\.npmrc|GITHUB_TOKEN)' range.diff | head -40
```

- The tarball is what runs. A change in shipped `dist/` or minified files with no matching change
  in the upstream source for the same range is the highest-signal finding — score `3`.
- `markdownlint-cli2` and `commitlint` read repository files; a new outbound call is
  disproportionate for either.

## Axis S — dependency and capability surface

```sh
for v in "$BASE" "$CAND"; do
  jq --arg v "$v" '.versions[$v]
    | {lifecycle: (.scripts // {} | with_entries(select(.key | test("^(pre|post)?install$|^prepare$")))),
       bin, dependencies, optionalDependencies, os, cpu}' meta.json > "$v.surface.json"
done
diff "$BASE.surface.json" "$CAND.surface.json"
```

- `scripts` の残り（lint や test など）は install 時に走らないので、ここでは見ない。
- **A new `preinstall` / `install` / `postinstall`** fails the install by design here
  (`strictDepBuilds` with an empty allowlist), so it cannot run silently — but it is still a `3`
  on this axis: a package that starts needing install-time code is the shape of the attack.
- New dependencies are new candidates. Each one enters the lockfile and is caught by the same
  transitive window; name them in the report.

## Reporting notes

- **Exposure**: the tools run inside the `node_tools` container with the repository mounted, and
  `RUNNER_MODE=container` passes the caller's `GITHUB_TOKEN` into it（`.makefiles/runner.mk`）.
  Report it as build / dev-time with a token in reach, not as build-time only.
- When `make node-tools-lock` was refused, say which package and version pnpm named, and that the
  answer is to wait for the window or to pin the direct dependency to a version whose tree is aged.
