// commit message の検査。**機械が課すのはこの3つだけである。**
//
// 文体・長さ・構成を機械に課さないのは、課すと「規約を満たすが伝わらない」メッセージを量産する
// 側へ倒れるためである。50文字に収めるために情報を落とした要約も、体裁だけ整えた中身の無い要約も、
// 検査を通る。**検査が通ることと目的が達成されることの乖離を、検査自身が隠す。**
//
// 一方で、種別の集合と「空でないこと」は機械が判定を誤らない。ここだけを機械に課し、残りは
// レビューで見る。
//
// `@commitlint/config-conventional` を extends していない。継承すると header-max-length /
// subject-case / subject-full-stop / footer 関連が同時に有効になり、上の切り分けが崩れる。
// **`extends` の不在は決定であって、書き忘れではない。**
//
// merge commit と `git revert` の生成物は commitlint の defaultIgnores が落とす。
// **`Revert: ` と `Revert "元のsubject"` は別物である。** 前者はこのリポジトリの種別であり
// 検査の対象、後者は `git revert` が生成する形で ignore される。defaultIgnores の revert
// パターンはコロンを伴わないため、この区別は自動的に成立する。「revert は除外」と思い込んで
// 自前で実装すると、両方を通してしまう。
module.exports = {
  rules: {
    // 種別の集合。先頭を大文字にする。`CI` のように全大文字となるものが混ざるため、
    // 表記の規則（type-case）は課さない —— この集合そのものが表記を固定する。
    "type-enum": [
      2,
      "always",
      [
        "Feat",
        "Fix",
        "Refactor",
        "Perf",
        "Docs",
        "Test",
        "Build",
        "CI",
        "Chore",
        "Style",
        "Revert",
      ],
    ],
    "type-empty": [2, "never"],
    "subject-empty": [2, "never"],
  },
};
