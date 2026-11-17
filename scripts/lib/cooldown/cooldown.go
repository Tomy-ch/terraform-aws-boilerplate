// Package cooldown は、供給網の窓の日数を持ちます。
//
// **窓を読む実装は複数ある**（scripts/tool-cooldown の gate と、scripts/versions が
// docker/tools/node/pnpm-workspace.yaml へ書く pnpm の minimumReleaseAge）。同じ日数を各々が
// 定数で持つと、片方だけを変えたときに窓が黙って割れる。
package cooldown

const (
	// ReleaseWindowDays は、GitHub Release から取る道具の窓（日）。
	ReleaseWindowDays = 14
	// RegistryWindowDays は、パッケージレジストリから取る道具の窓（日）。
	RegistryWindowDays = 7
)
