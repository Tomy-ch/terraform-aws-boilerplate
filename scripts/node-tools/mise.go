package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errMiseUsage   = xerrors.New("usage: node-tools mise-npm-disabled（引数は取りません）")
	errMiseNoTools = xerrors.New("mise ls が道具を1つも返しません")
	errMiseJSON    = xerrors.New("mise ls の出力を解釈できません")
	errMiseNPM     = xerrors.New("mise が npm: の道具を入れる対象にしています")
)

// runMiseNPMDisabled は、mise ls が返す、いまの作業ディレクトリで有効な道具に npm: のものが無いことを検証します。
// mise.toml が npm: の道具を宣言していることと、外す集合がその道具と一致することは versions check が見る。
// ここが見るのは、mise がその宣言を実際に守るかだけである。
func runMiseNPMDisabled(args []string, d deps) error {
	if len(args) > 0 {
		return errMiseUsage
	}
	cmd := d.tool("", stepMiseLs)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, d.stderr
	if err := cmd.Run(); err != nil {
		return xerrors.Wrap(err, "mise ls")
	}
	active, err := npmTools(out.Bytes())
	if err != nil {
		return err
	}
	if len(active) > 0 {
		return xerrors.Wrap(errMiseNPM, strings.Join(active, " "))
	}
	fmt.Fprintln(d.stderr, "✅ mise-npm-disabled: mise は npm: の道具を入れる対象にしていません")

	return nil
}

// npmTools は、mise ls --json の出力から npm: で始まる道具の名前を整列して返します。
// 道具が0件の出力は、mise が何も見ていない状態と区別できないので失敗にする。
func npmTools(b []byte) ([]string, error) {
	var tools map[string]json.RawMessage
	if err := json.Unmarshal(b, &tools); err != nil {
		return nil, xerrors.Wrap(errMiseJSON, err.Error())
	}
	if len(tools) == 0 {
		return nil, errMiseNoTools
	}
	var active []string
	for name := range tools {
		if strings.HasPrefix(name, "npm:") {
			active = append(active, name)
		}
	}
	sort.Strings(active)

	return active, nil
}
