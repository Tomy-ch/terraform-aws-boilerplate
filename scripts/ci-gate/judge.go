package main

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// changesJob は、gate の workflow で走るべき検査を決める job の id。
const changesJob = "changes"

func callerJob(name string) string { return checksdecl.CallerJob(name) }

func callerJobs(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, callerJob(n))
	}
	return out
}

var (
	// errNeeds は、needs の JSON を解釈できないか、宣言と job の集合が食い違うことを表す。
	errNeeds = xerrors.New("gate が受け取った job の集合が宣言と合いません")
	// errFailed は、判定が合格でないことを表す。
	errFailed = xerrors.New("gate の判定が合格ではありません")
)

type need struct {
	Result  string            `json:"result"`
	Outputs map[string]string `json:"outputs"`
}

// row は、判定の表の1行。job summary に全件を出す。
type row struct {
	Name     string
	Required bool
	Should   bool
	Result   string
	OK       bool
}

func parseNeeds(raw string) (map[string]need, error) {
	if raw == "" {
		return nil, xerrors.Wrap(errNeeds, "needs が空です")
	}
	var n map[string]need
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		return nil, xerrors.Wrapf(errNeeds, "%v", err)
	}
	if len(n) == 0 {
		return nil, xerrors.Wrap(errNeeds, "needs に job がありません")
	}
	return n, nil
}

// sameSet は、want と got の key の集合が一致するかを両方向で見る。
func sameSet(want []string, got map[string]need) error {
	var missing, extra []string
	for _, w := range want {
		if _, ok := got[w]; !ok {
			missing = append(missing, w)
		}
	}
	for g := range got {
		if !slices.Contains(want, g) {
			extra = append(extra, g)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		sort.Strings(extra)
		return xerrors.Wrapf(errNeeds, "宣言にあり needs に無い: [%s] / needs にあり宣言に無い: [%s]", checksdecl.JoinNames(missing), checksdecl.JoinNames(extra))
	}
	return nil
}

// judgePR は、Pull Request の gate の判定。should は gate が自分で決め直した走るべき検査で、changes の job の出力
// （run）と一致しなければならない。changes の出力を信じて判定すると、それが過小なときに検査を省いたまま合格する。
//
// 合格の条件:
//   - needs の job の集合が、宣言の検査と changes の job に一致する。
//   - changes の job が success で、出力が should と一致する。
//   - required の検査のうち、走るべきものは success、走るべきでないものは skipped か success。
//
// required でない検査は表に載せるが、合否に数えない。
func judgePR(d checksdecl.Decl, should []string, needs map[string]need) ([]row, error) {
	if err := sameSet(append([]string{changesJob}, callerJobs(d.Names())...), needs); err != nil {
		return nil, err
	}
	ch := needs[changesJob]
	var rows []row
	failed := 0
	if ch.Result != "success" {
		failed++
		rows = append(rows, row{Name: changesJob, Required: true, Should: true, Result: ch.Result})
	} else {
		var got []string
		if err := json.Unmarshal([]byte(ch.Outputs["run"]), &got); err != nil || !slices.Equal(got, should) {
			failed++
			rows = append(rows, row{Name: changesJob + "（出力の照合）", Required: true, Should: true, Result: "mismatch"})
		} else {
			rows = append(rows, row{Name: changesJob, Required: true, Should: true, Result: ch.Result, OK: true})
		}
	}
	for _, c := range d.Checks {
		r := row{Name: c.Name, Required: c.IsRequired(), Should: slices.Contains(should, c.Name), Result: needs[callerJob(c.Name)].Result}
		switch {
		case r.Should:
			r.OK = r.Result == "success"
		default:
			r.OK = r.Result == "skipped" || r.Result == "success"
		}
		if !r.Required {
			rows = append(rows, r)
			continue
		}
		if !r.OK {
			failed++
		}
		rows = append(rows, r)
	}
	if failed > 0 {
		return rows, xerrors.Wrapf(errFailed, "%d 件", failed)
	}
	return rows, nil
}

// treeChecks は、線の検証が走らせる検査の名前を返す。どれを走らせるかは checksdecl の TreeChecks が決める。
func treeChecks(d checksdecl.Decl) []string { return d.TreeChecks() }

// judgeLine は、線の検証の判定。needs の集合が treeChecks と一致し、すべて success のときだけ合格する。
func judgeLine(d checksdecl.Decl, needs map[string]need) ([]row, error) {
	want := treeChecks(d)
	if len(want) == 0 {
		return nil, xerrors.Wrap(checksdecl.ErrNoChecks, "scope が tree の required の検査がありません")
	}
	if err := sameSet(callerJobs(want), needs); err != nil {
		return nil, err
	}
	var rows []row
	failed := 0
	for _, name := range want {
		r := row{Name: name, Required: true, Should: true, Result: needs[callerJob(name)].Result}
		r.OK = r.Result == "success"
		if !r.OK {
			failed++
		}
		rows = append(rows, r)
	}
	if failed > 0 {
		return rows, xerrors.Wrapf(errFailed, "%d 件", failed)
	}
	return rows, nil
}
