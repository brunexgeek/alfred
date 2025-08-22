package catalog

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"hash/maphash"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Catalog struct {
	Products     map[string]*Product `json:"-"`
	Publications []*Publication      `json:"pubs,omitempty"`
	seed         maphash.Seed        `json:"-"`
}

type Product struct {
	Name         string  // unique product name (lowercase)
	Latest       Version // latest version in semantic version format
	Publications []*Publication
}

type Publication struct {
	Product      string     `json:"prod"` // unique product name (lowercase)
	ShortVersion Version    `json:"sver"` // only major and minor
	Version      Version    `json:"ver"`  // complete semantic version
	Format       FormatType `json:"fmt"`
	Language     Language   `json:"lang"`
	Date         time.Time  `json:"date"`
	hash         uint64     `json:"-"`
}

func (c *Catalog) add_product(pub *Publication) {
	result := c.Products[pub.Product]
	if result == nil {
		result = &Product{Name: pub.Product, Publications: make([]*Publication, 0)}
		c.Products[pub.Product] = result
	}
	result.Publications = append(result.Publications, pub)
	if pub.Version.Newer(result.Latest) {
		result.Latest = pub.Version
	}

}

func (p Publication) Validate() error {
	if name_re == nil || !name_re.MatchString(p.Product) {
		return fmt.Errorf("Invalid product name")
	}
	if p.Language != "pt" && p.Language != "en" && p.Language != "es" {
		return fmt.Errorf("Unsupported language")
	}
	if !p.Format.IsValid() {
		return fmt.Errorf("Unsupported format")
	}
	if !p.Version.IsFull() {
		return fmt.Errorf("Incomplete or invalid semantic version")
	}
	return nil
}

type Version struct {
	Major int
	Minor int
	Fix   int
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

type FormatType string

const (
	HTML FormatType = "html"
	PDF  FormatType = "pdf"
	TGZ  FormatType = "tgz"
)

func (v FormatType) IsValid() bool {
	return v == HTML || v == PDF || v == TGZ
}

type Language string

const (
	PT Language = "pt"
	ES Language = "es"
	EN Language = "en"
)

func Open(fpath string) (*Catalog, error) {
	output := &Catalog{seed: maphash.MakeSeed(), Products: make(map[string]*Product)}

	info, err := os.Stat(fpath)
	if err != nil {
		return output, nil
	} else if info.IsDir() {
		return nil, fmt.Errorf("'%s' must be a regular file", fpath)
	}

	file, err := os.OpenFile(fpath, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, &output)
	if err != nil {
		return nil, err
	}

	for _, pub := range output.Publications {
		output.add_product(pub)
	}

	return output, nil
}

func (c *Catalog) Save(fpath string) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(fpath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(data)
	if err != nil {
		return err
	}
	return nil
}

func (p *Publication) MetaPath() string {
	return path.Join(p.Product, string(p.Format))
}

func (p *Publication) DataPath() string {
	return path.Join(p.Product, string(p.Format), p.ShortVersion.ToString(), string(p.Language))
}

func (p *Publication) Hash() uint64 {
	if p.hash != 0 {
		return p.hash
	}
	h := fnv.New64a()
	h.Write([]byte(p.Product))
	h.Write([]byte(p.Format))
	h.Write([]byte(p.Language))
	h.Write([]byte(p.Version.ToString()))
	p.hash = h.Sum64()
	return p.hash
}

func (c *Catalog) AddPublication(pub *Publication) {
	hash := pub.Hash()

	// TODO: add non-HTML publication only if there's a HTML publication in the same version

	for i, item := range c.Publications {
		if item.Hash() == hash {
			c.Publications[i] = pub
			return
		}
	}

	c.Publications = append(c.Publications, pub)
	c.add_product(pub)
}
