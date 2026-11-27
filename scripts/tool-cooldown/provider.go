package main

import (
	"context"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// providerLockFile は provider の版の正本の lock。root の lock はその写しなので、ここだけを読む。
	providerLockFile = tflock.Dir + "/" + tflock.LockFile
	// providerBackend は正本の provider の key の接頭辞。後ろに `host/namespace/type` が続く。
	providerBackend = "terraform-registry"

	terraformRegistryBase = "https://" + tflock.Registry + "/"
)

var providerVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// parseProviderLock は、正本の lock の provider を道具として返します。版は lock が固定したもので、init が
// 実際に取る版です。
func parseProviderLock(content []byte) ([]tool, error) {
	providers, err := tflock.ParseLock(content, providerLockFile)
	if err != nil {
		return nil, err
	}
	tools := make([]tool, 0, len(providers))
	for _, p := range providers {
		// 版は Registry の URL の path に入るので、完全一致の版の形だけを受ける。
		if !providerVersionRe.MatchString(p.Version) {
			return nil, xerrors.Wrapf(tflock.ErrInvalidLock, "%s: %s の版 %q は X.Y.Z ではありません", providerLockFile, p.Addr, p.Version)
		}
		tools = append(tools, tool{key: providerBackend + ":" + p.Addr, version: p.Version})
	}

	return tools, nil
}

// registryProvider は `host/namespace/type` を namespace/type に分けます。host が Terraform Registry でないものは、
// 公開時刻の取得元を持たないのでエラーです。
func registryProvider(addr string) (string, error) {
	host, rest, ok := strings.Cut(addr, "/")
	if !ok || host != tflock.Registry || strings.Count(rest, "/") != 1 {
		return "", xerrors.Wrap(errUnsupportedBackend, providerBackend+":"+addr)
	}

	return rest, nil
}

// registryProviderAt は、Terraform Registry の v1 API が返すその版の公開時刻です。
func registryProviderAt(ctx context.Context, client *http.Client, addr, version string) (time.Time, error) {
	name, err := registryProvider(addr)
	if err != nil {
		return time.Time{}, err
	}
	var body struct {
		//nolint:tagliatelle // Terraform Registry API の応答フィールド名
		PublishedAt time.Time `json:"published_at"`
	}
	if err := getJSON(ctx, client, terraformRegistryBase+path.Join("v1/providers", name, version), &body); err != nil {
		return time.Time{}, err
	}
	if body.PublishedAt.IsZero() {
		return time.Time{}, xerrors.Wrap(errNotFound, addr+"@"+version)
	}

	return body.PublishedAt, nil
}

// registryProviderReleases は、Terraform Registry の v2 API が返す版の一覧です。
func registryProviderReleases(ctx context.Context, client *http.Client, addr string) ([]release, error) {
	name, err := registryProvider(addr)
	if err != nil {
		return nil, err
	}
	var body struct {
		Included []struct {
			Type       string `json:"type"`
			Attributes struct {
				Version string `json:"version"`
				//nolint:tagliatelle // Terraform Registry API の応答フィールド名
				PublishedAt time.Time `json:"published-at"`
			} `json:"attributes"`
		} `json:"included"`
	}
	if err := getJSON(ctx, client, terraformRegistryBase+path.Join("v2/providers", name)+"?include=provider-versions", &body); err != nil {
		return nil, err
	}
	out := make([]release, 0, len(body.Included))
	for _, v := range body.Included {
		if v.Type != "provider-versions" || !stableVersion(v.Attributes.Version) || v.Attributes.PublishedAt.IsZero() {
			continue
		}
		out = append(out, release{version: v.Attributes.Version, at: v.Attributes.PublishedAt})
	}
	if len(out) == 0 {
		return nil, xerrors.Wrap(errNotFound, addr)
	}

	return out, nil
}
