package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/hashicorp/go-version"
	"golang.org/x/mod/modfile"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	providerRepo  = "hashicorp/terraform-provider-aws"
	sdkModule     = "github.com/hashicorp/aws-sdk-go-base/v2"
	sdkRepo       = "hashicorp/aws-sdk-go-base"
	endpointsPath = "endpoints/endpoints_gen.go"
	maxBody       = 16 << 20
)

var (
	errHTTP          = xerrors.New("上流から取得できません")
	errNoVersions    = xerrors.New("registry の版の一覧が空です")
	errBadVersion    = xerrors.New("registry の版を解釈できません")
	errBadConstraint = xerrors.New("版の制約を解釈できません")
	errNoMatch       = xerrors.New("制約を満たす正式版がありません")
	errGoMod         = xerrors.New("provider の go.mod から aws-sdk-go-base の版を導けません")
)

// upstream の registry と raw は、テストが httptest の URL へ差し替えます。
type upstream struct {
	client   *http.Client
	registry string
	raw      string
}

func (u upstream) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, xerrors.Wrapf(errHTTP, "%s: %v", url, err)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, xerrors.Wrapf(errHTTP, "%s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, xerrors.Wrapf(errHTTP, "%s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, xerrors.Wrapf(errHTTP, "%s: %v", url, err)
	}
	if len(body) > maxBody {
		return nil, xerrors.Wrapf(errHTTP, "%s: 本文が %d バイトの上限を超えています", url, maxBody)
	}

	return body, nil
}

func (u upstream) providerVersions(ctx context.Context, source string) ([]string, error) {
	body, err := u.get(ctx, u.registry+"/v1/providers/"+source+"/versions")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, xerrors.Wrapf(errBadVersion, "%v", err)
	}
	if len(doc.Versions) == 0 {
		return nil, errNoVersions
	}
	versions := make([]string, 0, len(doc.Versions))
	for _, v := range doc.Versions {
		if v.Version == "" {
			return nil, xerrors.Wrapf(errBadVersion, "version の無い要素があります")
		}
		versions = append(versions, v.Version)
	}

	return versions, nil
}

// latestMatching は prerelease を候補から外します。制約が prerelease を名指す形は扱わず、満たす正式版が
// 無ければ errNoMatch で失敗します。
func latestMatching(versions []string, constraint string) (string, error) {
	c, err := version.NewConstraint(constraint)
	if err != nil {
		return "", xerrors.Wrapf(errBadConstraint, "%q: %v", constraint, err)
	}
	var best *version.Version
	for _, raw := range versions {
		v, err := version.NewVersion(raw)
		if err != nil {
			return "", xerrors.Wrapf(errBadVersion, "%q: %v", raw, err)
		}
		if v.Prerelease() != "" || !c.Check(v) {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best = v
		}
	}
	if best == nil {
		return "", xerrors.Wrapf(errNoMatch, "%q", constraint)
	}

	return best.Original(), nil
}

func (u upstream) sdkVersion(ctx context.Context, providerVersion string) (string, string, error) {
	url := u.raw + "/" + providerRepo + "/v" + providerVersion + "/go.mod"
	body, err := u.get(ctx, url)
	if err != nil {
		return "", url, err
	}
	v, err := sdkVersionFromGoMod(body)

	return v, url, err
}

// sdkVersionFromGoMod は、replace が aws-sdk-go-base を差し替えていれば失敗します。require の版は、
// そのとき provider が実際に使うものではないからです。
func sdkVersionFromGoMod(data []byte) (string, error) {
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", xerrors.Wrapf(errGoMod, "%v", err)
	}
	for _, r := range f.Replace {
		if r.Old.Path == sdkModule {
			return "", xerrors.Wrapf(errGoMod, "replace が %s を差し替えています", sdkModule)
		}
	}
	for _, r := range f.Require {
		if r.Mod.Path == sdkModule {
			return r.Mod.Version, nil
		}
	}

	return "", xerrors.Wrapf(errGoMod, "require に %s がありません", sdkModule)
}

func (u upstream) endpoints(ctx context.Context, sdkVersion string) ([]byte, string, error) {
	url := u.raw + "/" + sdkRepo + "/" + sdkVersion + "/" + endpointsPath
	body, err := u.get(ctx, url)

	return body, url, err
}
