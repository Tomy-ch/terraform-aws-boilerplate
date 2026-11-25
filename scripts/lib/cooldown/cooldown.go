// Package cooldown は、供給網の窓の日数を持ちます。
//
// **窓を読む実装は複数ある**（scripts/tool-cooldown と scripts/go-cooldown の gate、
// scripts/versions が docker/tools/node/pnpm-workspace.yaml へ書く pnpm の minimumReleaseAge、
// scripts/pin-images と scripts/pin-actions の resolve）。
// 同じ日数を各々が定数で持つと、片方だけを変えたときに窓が黙って割れる。
package cooldown

const (
	// ReleaseWindowDays は、GitHub Release から取る道具の窓（日）。
	ReleaseWindowDays = 14
	// RegistryWindowDays は、パッケージレジストリから取る道具の窓（日）。
	RegistryWindowDays = 7
	// PinWindowDays は、Docker image の digest と GitHub Actions の SHA の固定を解決し直すときの窓（日）。
	PinWindowDays = 14
)
