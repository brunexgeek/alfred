package catalog

import (
	"fmt"
	"regexp"
	"strconv"
)

type Version struct {
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Patch int    `json:"patch"`
	Tag   string `json:"tag"`
}

var version_re, _ = regexp.Compile(`^(\d{1,4})\.(\d{1,4})\.(\d{1,4})(?:-([A-Za-z][A-Za-z0-9_-]*))?$`)

func ParseVersion(value string) (Version, error) {
	parts := version_re.FindStringSubmatch(value)
	if parts == nil {
		return Version{}, fmt.Errorf("invalid version information")
	}

	result := Version{}
	result.Major, _ = strconv.Atoi(parts[1])
	result.Minor, _ = strconv.Atoi(parts[2])
	result.Patch, _ = strconv.Atoi(parts[3])
	result.Tag = parts[4]
	return result, nil
}

func (v Version) ToString() string {
	if v.Tag != "" {
		return fmt.Sprintf("%d.%d.%d-%s", v.Major, v.Minor, v.Patch, v.Tag)
	} else {
		return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	}
}

func (v Version) ToSortableString() string {
	if v.Tag != "" {
		return fmt.Sprintf("%04d.%04d.%04d-%s", v.Major, v.Minor, v.Patch, v.Tag)
	} else {
		return fmt.Sprintf("%04d.%04d.%04d", v.Major, v.Minor, v.Patch)
	}
}

func (v Version) IsNewer(o Version) bool {
	return v.Major > o.Major || v.Minor > o.Minor || v.Patch > o.Patch
}

func CompareVersions(a Version, b Version) int {
	if a.Major < b.Major {
		return -1
	}
	if a.Major > b.Major {
		return 1
	}

	if a.Minor < b.Minor {
		return -1
	}
	if a.Minor > b.Minor {
		return 1
	}

	if a.Patch < b.Patch {
		return -1
	}
	if a.Patch > b.Patch {
		return 1
	}

	return 0
}

func (v *Version) HasVersion() bool {
	return !(v.Major <= 0 && v.Minor <= 0 && v.Patch <= 0)
}
