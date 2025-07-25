package alertmanager

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/prometheus/prometheus/model/rulefmt"
	"google.golang.org/protobuf/types/known/durationpb"
	"gopkg.in/yaml.v3"
)

func ParseRules(r io.Reader, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	var groups rulefmt.RuleGroups
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)
	if err := decoder.Decode(&groups); err != nil {
		return nil, fmt.Errorf("parsing alert rules: %v", err)
	}
	return ToGroups(groups.Groups, logQlParser)
}

func ToGroups(groups []rulefmt.RuleGroup, parser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	out := make([]*typesv1.AlertGroup, 0, len(groups))
	for _, g := range groups {
		ag, err := ToGroup(g, parser)
		if err != nil {
			return nil, fmt.Errorf("group %q: %v", g.Name, err)
		}
		out = append(out, ag)
	}
	return out, nil
}

func ToGroup(g rulefmt.RuleGroup, parser func(string) (*typesv1.Query, error)) (*typesv1.AlertGroup, error) {
	out := &typesv1.AlertGroup{
		Name:     g.Name,
		Interval: durationpb.New(time.Duration(g.Interval)),
		Limit:    int32(g.Limit),
		Labels:   mapToObj(g.Labels),
		Rules:    make([]*typesv1.AlertRule, 0, len(g.Rules)),
	}
	if g.QueryOffset != nil {
		out.QueryOffset = durationpb.New(time.Duration(*g.QueryOffset))
	}
	for _, a := range g.Rules {
		ar, err := ToAlert(a, parser)
		if err != nil {
			return nil, fmt.Errorf("alert %q: %v", a.Alert, err)
		}
		out.Rules = append(out.Rules, ar)
	}
	return out, nil
}

func ToAlert(ar rulefmt.Rule, parser func(string) (*typesv1.Query, error)) (*typesv1.AlertRule, error) {
	q, err := parser(ar.Expr)
	if err != nil {
		return nil, err
	}
	out := &typesv1.AlertRule{
		Name:        ar.Alert,
		Expr:        q,
		For:         durationpb.New(time.Duration(ar.For)),
		Labels:      mapToObj(ar.Labels),
		Annotations: mapToObj(ar.Annotations),
	}
	if ar.KeepFiringFor != 0 {
		out.KeepFiringFor = durationpb.New(time.Duration(ar.KeepFiringFor))
	}
	return out, nil
}

func mapToObj(m map[string]string) *typesv1.Obj {
	out := make([]*typesv1.KV, 0, len(m))
	for k, v := range m {
		out = append(out, typesv1.KeyVal(
			k, typesv1.ValStr(v),
		))
	}
	slices.SortFunc(out, func(a, b *typesv1.KV) int {
		return strings.Compare(a.Key, b.Key)
	})
	return &typesv1.Obj{Kvs: out}
}
