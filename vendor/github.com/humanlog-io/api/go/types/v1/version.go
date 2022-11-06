package typesv1

import (
	"github.com/blang/semver"
)

func (v Version) AsSemver() (semver.Version, error) {
	out := semver.Version{
		Major: uint64(v.Major),
		Minor: uint64(v.Minor),
		Patch: uint64(v.Patch),
	}
	if len(v.Prereleases) > 0 {

		out.Pre = make([]semver.PRVersion, 0, len(v.Prereleases))
		for _, pre := range v.Prereleases {
			pr, err := semver.NewPRVersion(pre)
			if err != nil {
				return out, err
			}
			out.Pre = append(out.Pre, pr)
		}
	}
	if v.Build != "" {
		out.Build = []string{v.Build}
	}
	return out, nil
}
