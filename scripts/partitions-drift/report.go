package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// drift の missing は上流に在って写しに無いもの、extra は写しに在って上流に無いものです。
type drift struct {
	missingPartitions []string
	extraPartitions   []string
	missingRegions    map[string][]string
	extraRegions      map[string][]string
}

func (d drift) empty() bool {
	return len(d.missingPartitions) == 0 && len(d.extraPartitions) == 0 &&
		len(d.missingRegions) == 0 && len(d.extraRegions) == 0
}

type report struct {
	constraint   string
	provider     string
	sdk          string
	goModURL     string
	endpointsURL string
	compared     bool
	drift        drift
	err          error
}

func (r report) verdict() error {
	switch {
	case r.err != nil:
		return r.err
	case !r.compared:
		return errNotCompared
	case !r.drift.empty():
		return errDrift
	default:
		return nil
	}
}

// compare は、両方に在る partition についてだけ region を比べます。片方にしか無い partition の region は、
// partition の差として1行で報告します。
func compare(up, cp partitionSet) drift {
	d := drift{missingRegions: map[string][]string{}, extraRegions: map[string][]string{}}
	for _, p := range slices.Sorted(maps.Keys(up)) {
		cr, ok := cp[p]
		if !ok {
			d.missingPartitions = append(d.missingPartitions, p)

			continue
		}
		if m := minus(up[p], cr); len(m) > 0 {
			d.missingRegions[p] = m
		}
		if e := minus(cr, up[p]); len(e) > 0 {
			d.extraRegions[p] = e
		}
	}
	for _, p := range slices.Sorted(maps.Keys(cp)) {
		if _, ok := up[p]; !ok {
			d.extraPartitions = append(d.extraPartitions, p)
		}
	}

	return d
}

func minus(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}

	return out
}

func renderSummary(r report) string {
	var b strings.Builder
	switch err := r.verdict(); {
	case err == nil:
		b.WriteString("## ✅ partitions-drift: 写しは上流の一覧と一致しています\n\n")
	case r.compared:
		b.WriteString("## ⚠️ partitions-drift: 写しが上流の一覧と一致しません\n\n")
	default:
		fmt.Fprintf(&b, "## ❌ partitions-drift: 上流と比べられませんでした\n\n```text\n%v\n```\n\n", err)
	}

	writeField(&b, "版の制約", r.constraint)
	writeField(&b, "比べた hashicorp/aws", r.provider)
	writeField(&b, "その aws-sdk-go-base", r.sdk)
	writeField(&b, "go.mod", r.goModURL)
	writeField(&b, "endpoints", r.endpointsURL)

	if r.compared && !r.drift.empty() {
		b.WriteString("\n| 差 | partition | region |\n| --- | --- | --- |\n")
		for _, p := range r.drift.missingPartitions {
			fmt.Fprintf(&b, "| 写しに無い | `%s` | |\n", p)
		}
		for _, p := range r.drift.extraPartitions {
			fmt.Fprintf(&b, "| 上流に無い | `%s` | |\n", p)
		}
		for _, p := range slices.Sorted(maps.Keys(r.drift.missingRegions)) {
			fmt.Fprintf(&b, "| 写しに無い | `%s` | `%s` |\n", p, strings.Join(r.drift.missingRegions[p], "`, `"))
		}
		for _, p := range slices.Sorted(maps.Keys(r.drift.extraRegions)) {
			fmt.Fprintf(&b, "| 上流に無い | `%s` | `%s` |\n", p, strings.Join(r.drift.extraRegions[p], "`, `"))
		}
	}

	return b.String()
}

func writeField(b *strings.Builder, name, value string) {
	if value != "" {
		fmt.Fprintf(b, "- %s: %s\n", name, value)
	}
}
