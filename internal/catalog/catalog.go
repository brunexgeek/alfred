package catalog

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type LanguageCode string

const (
	PT LanguageCode = "pt"
	ES LanguageCode = "es"
	EN LanguageCode = "en"
)

type FormatCode string

const (
	HTML FormatCode = "html"
	PDF  FormatCode = "pdf"
	TGZ  FormatCode = "tgz"
)

type Catalog struct {
	Products map[string]*ProductEntry `json:"products"`
}

type ProductEntry struct {
	Name      string                          `json:"name"` // unique product name (lowercase)
	Tainted   bool                            `json:"-"`
	Languages map[LanguageCode]*LanguageEntry `json:"languages"` // indexed by language code
}

type LanguageEntry struct {
	Language LanguageCode             `json:"-"`
	Tainted  bool                     `json:"-"`
	Latest   Version                  `json:"latest"`
	Versions map[string]*VersionEntry `json:"versions"` // indexed by semantic version
}

type VersionEntry struct {
	Version Version                     `json:"-"`
	Tainted bool                        `json:"-"`
	Formats map[FormatCode]*FormatEntry `json:"formats"` // indexed by format code
}

type FormatEntry struct {
	Format FormatCode `json:"-"`
	Path   string     `json:"path"`
}

type Publication struct {
	Product  string       `json:"prod"` // unique product name (lowercase)
	Version  Version      `json:"ver"`  // complete semantic version
	Format   FormatCode   `json:"fmt"`
	Language LanguageCode `json:"lang"`
	Date     time.Time    `json:"date"`
	hash     uint64       `json:"-"`
}

func NewCatalog() *Catalog {
	return &Catalog{make(map[string]*ProductEntry)}
}

func (p Publication) Validate() error {
	if name_re == nil || !name_re.MatchString(p.Product) {
		return fmt.Errorf("invalid product name")
	}
	if !p.Language.IsValid() {
		return fmt.Errorf("unsupported language")
	}
	if !p.Format.IsValid() {
		return fmt.Errorf("unsupported format")
	}
	if !p.Version.IsFull() {
		return fmt.Errorf("incomplete or invalid semantic version")
	}
	return nil
}

type Version struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
	Fix   int `json:"fix"`
}

const RE_PRODUCT_NAME = "([a-z][a-z0-9_]{0,31})"
const RE_SHORT_VERSION = "([0-9]{1,3}\\.[0-9]{1,3})"
const RE_URL_VERSION = "([0-9]{1,3}\\.[0-9]{1,3}|latest)"
const RE_FORMAT = "(" + HTML + "|" + PDF + "|" + TGZ + ")"
const RE_LANGUAGE = "(" + PT + "|" + ES + "|" + EN + ")"

var name_re, _ = regexp.Compile("^" + RE_PRODUCT_NAME + "$")
var full_re, _ = regexp.Compile("^[0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3}$")
var short_re, _ = regexp.Compile("^" + RE_SHORT_VERSION + "$")

func ParseVersion(value string) (Version, error) {
	if !IsValid(value) {
		return Version{}, fmt.Errorf("Invalid semantic version")
	}
	parts := strings.Split(value, ".")
	result := Version{}
	result.Major, _ = strconv.Atoi(parts[0])
	result.Minor, _ = strconv.Atoi(parts[1])
	if len(parts) == 3 {
		result.Fix, _ = strconv.Atoi(parts[2])
	}
	return result, nil
}

func (v Version) IsFull() bool {
	return v.Major >= 0 && v.Minor >= 0 && v.Fix >= 0
}

func (v Version) IsShort() bool {
	return v.Major >= 0 && v.Minor >= 0 && v.Fix == -1
}

func (v Version) IsValid() bool {
	return v.IsShort() || v.IsFull()
}

func IsFull(v string) bool {
	return full_re != nil && full_re.MatchString(v)
}

func IsShort(v string) bool {
	return short_re != nil && short_re.MatchString(v)
}

func IsValid(v string) bool {
	return IsShort(v) || IsFull(v)
}

func (v Version) GetShortVersion() Version {
	if v.IsFull() {
		return Version{Major: v.Major, Minor: v.Minor, Fix: -1}
	} else {
		return v
	}
}

func (v Version) ToString() string {
	if v.Fix < 0 {
		return fmt.Sprintf("%d.%d", v.Major, v.Minor)
	} else {
		return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Fix)
	}
}

func (v Version) Newer(o Version) bool {
	if v.Fix < 0 {
		v.Fix = 0
	}
	if o.Fix < 0 {
		o.Fix = 0
	}
	return v.Major > o.Major || v.Minor > o.Minor || v.Fix > o.Fix
}

func (v FormatCode) IsValid() bool {
	return v == HTML || v == PDF || v == TGZ
}

func (v LanguageCode) IsValid() bool {
	return v == PT || v == ES || v == EN
}

func (c *Catalog) Serialize() ([]byte, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (p *Publication) DataPath() string {
	return "/" + path.Join(p.Product, string(p.Language), p.Version.ToString(), string(p.Format))
}

func (p *Publication) Hash() uint64 {
	if p.hash != 0 {
		return p.hash
	}
	h := fnv.New64a()
	h.Write([]byte(p.Product))
	h.Write([]byte(p.Language))
	h.Write([]byte(p.Version.ToString()))
	h.Write([]byte(p.Format))
	p.hash = h.Sum64()
	return p.hash
}

func (c *Catalog) AddPublication(pub *Publication) {
	ok := false
	var product *ProductEntry
	var language *LanguageEntry
	var version *VersionEntry

	if product, ok = c.Products[pub.Product]; !ok {
		product = &ProductEntry{pub.Product, true, make(map[LanguageCode]*LanguageEntry, 0)}
		c.Products[pub.Product] = product
	}

	if language, ok = product.Languages[pub.Language]; !ok {
		language = &LanguageEntry{pub.Language, true, pub.Version, make(map[string]*VersionEntry, 0)}
		product.Languages[pub.Language] = language
	}

	if version, ok = language.Versions[pub.Version.ToString()]; !ok {
		version = &VersionEntry{pub.Version, true, make(map[FormatCode]*FormatEntry, 0)}
		language.Versions[pub.Version.ToString()] = version
	}

	if _, ok = version.Formats[pub.Format]; !ok {
		format := &FormatEntry{pub.Format, pub.DataPath()}
		version.Formats[pub.Format] = format
	}
}
