package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	appFile = ".github/settings/github-app.json"
	// appKeyEnv は App の秘密鍵（PEM）を渡す環境変数。ファイルにも引数にも置かない。
	appKeyEnv = "REPO_SETTINGS_APP_PRIVATE_KEY"
	// appEnvironment は秘密鍵を secret として持つ environment の名前。
	appEnvironment = "repo-settings"
	// GitHub は exp を iat から10分以内に限り、時計のずれを見込んで iat を過去へずらすことを勧める。
	jwtBackdate = 60 * time.Second
	jwtLifetime = 9 * time.Minute
)

var (
	errAppDecl     = xerrors.New("App の宣言が不正です")
	errAppKey      = xerrors.New("App の秘密鍵を読めません")
	errAppMismatch = xerrors.New("App の実態が宣言と一致しません。何も書いていません")
)

// appDecl は .github/settings/github-app.json —— 保護設定の適用に使う GitHub App の宣言（ADR-0603 決定30）。
type appDecl struct {
	AppID               int64             `json:"app_id"`
	Permissions         map[string]string `json:"permissions"`
	RepositorySelection string            `json:"repository_selection"`
}

// appPermissions は App に許す権限の全集合。Administration は ruleset・environment・deployment branch
// policy の書き込みに、Metadata はすべての App に要る。
var appPermissions = map[string]string{"administration": "write", "metadata": "read"}

// appSource は App の token を作るための外界。key は PEM、now は時刻。
type appSource struct {
	key string
	now func() time.Time
}

func readAppDecl(root string) (appDecl, error) {
	data, err := os.ReadFile(filepath.Clean(filepath.Join(root, appFile)))
	if err != nil {
		return appDecl{}, xerrors.Wrapf(err, "%s の読み取り", appFile)
	}
	var d appDecl
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return appDecl{}, xerrors.Wrapf(errAppDecl, "%s: %v", appFile, err)
	}
	if dec.More() {
		return appDecl{}, xerrors.Wrapf(errAppDecl, "%s: 値の後に続きがあります", appFile)
	}
	switch {
	case d.AppID <= 0:
		return appDecl{}, xerrors.Wrapf(errAppDecl, "%s: app_id は正の整数です", appFile)
	case !maps.Equal(d.Permissions, appPermissions):
		return appDecl{}, xerrors.Wrapf(errAppDecl, "%s: permissions は %v だけを持ちます", appFile, appPermissions)
	case d.RepositorySelection != "selected":
		return appDecl{}, xerrors.Wrapf(errAppDecl, "%s: repository_selection は selected です", appFile)
	}
	return d, nil
}

func parseAppKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, xerrors.Wrapf(errAppKey, "PEM ではありません")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, xerrors.Wrapf(errAppKey, "PKCS#1 でも PKCS#8 でもありません")
	}
	k, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, xerrors.Wrapf(errAppKey, "RSA の鍵ではありません")
	}
	return k, nil
}

// appJWT は App として GitHub へ名乗る JWT（RS256）を作ります。
func appJWT(key *rsa.PrivateKey, appID int64, now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-jwtBackdate).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		"iss": strconv.FormatInt(appID, 10),
	})
	if err != nil {
		return "", xerrors.Wrapf(errAppKey, "JWT の claims: %v", err)
	}
	signing := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", xerrors.Wrapf(errAppKey, "JWT の署名: %v", err)
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

// appClient は App の installation token を持つ client を返します。書き込みの前に、installation の権限と
// 対象リポジトリが宣言どおりであることを確かめ、token の権限を Administration の write と repo だけへ絞ります。
func appClient(ctx context.Context, c githubClient, root, repo string, src appSource) (githubClient, error) {
	decl, err := readAppDecl(root)
	if err != nil {
		return githubClient{}, err
	}
	if src.key == "" {
		return githubClient{}, xerrors.Wrapf(errAppKey, "%s が空です", appKeyEnv)
	}
	key, err := parseAppKey(src.key)
	if err != nil {
		return githubClient{}, err
	}
	jwt, err := appJWT(key, decl.AppID, src.now())
	if err != nil {
		return githubClient{}, err
	}
	asApp := githubClient{base: c.base, token: jwt, http: c.http}
	var inst struct {
		ID                  int64             `json:"id"`
		AppID               int64             `json:"app_id"`
		Permissions         map[string]string `json:"permissions"`
		RepositorySelection string            `json:"repository_selection"`
	}
	if err := asApp.do(ctx, http.MethodGet, "/repos/"+repo+"/installation", nil, &inst); err != nil {
		return githubClient{}, err
	}
	switch {
	case inst.ID <= 0:
		return githubClient{}, xerrors.Wrapf(errUnexpectedResponse, "installation の id がありません")
	case inst.AppID != decl.AppID:
		return githubClient{}, xerrors.Wrapf(errAppMismatch, "app_id: 実態 %d、宣言 %d", inst.AppID, decl.AppID)
	case !maps.Equal(inst.Permissions, decl.Permissions):
		return githubClient{}, xerrors.Wrapf(errAppMismatch, "permissions: 実態 %v、宣言 %v", inst.Permissions, decl.Permissions)
	case inst.RepositorySelection != decl.RepositorySelection:
		return githubClient{}, xerrors.Wrapf(errAppMismatch, "repository_selection: 実態 %q、宣言 %q", inst.RepositorySelection, decl.RepositorySelection)
	}
	_, name, _ := strings.Cut(repo, "/")
	var tok struct {
		Token string `json:"token"`
	}
	body := map[string]any{"repositories": []string{name}, "permissions": map[string]string{"administration": "write"}}
	if err := asApp.do(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", inst.ID), body, &tok); err != nil {
		return githubClient{}, err
	}
	if tok.Token == "" {
		return githubClient{}, xerrors.Wrapf(errUnexpectedResponse, "installation token がありません")
	}
	asInst := githubClient{base: c.base, token: tok.Token, http: c.http}
	var repos struct {
		TotalCount   *int `json:"total_count"`
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := asInst.do(ctx, http.MethodGet, "/installation/repositories", nil, &repos); err != nil {
		return githubClient{}, err
	}
	if repos.TotalCount == nil || *repos.TotalCount != 1 || len(repos.Repositories) != 1 || repos.Repositories[0].FullName != repo {
		return githubClient{}, xerrors.Wrapf(errAppMismatch, "token が届くリポジトリが %s の1件だけではありません", repo)
	}
	return asInst, nil
}
