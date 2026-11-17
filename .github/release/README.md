# リリースノート

`vX.Y.Z.md` を1つのリリースにつき1つ置く。**説明のための置き場所ではなく、タグの本文そのものである。**

`scripts/release` が `git tag -a <v> -F .github/release/<v>.md` で読み、`gh release create --notes-file`
で GitHub Release の本文にもする（`notePath` / `tagSteps`）。**対応するファイルが無ければタグが打てない。**

書き方の手順と節の構成は `.claude/skills/release-notes` が持つ。ここに再掲しない。
