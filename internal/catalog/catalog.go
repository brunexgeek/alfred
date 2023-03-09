package catalog

import "regexp"

type Catalog struct {
	Products []Product
}

type Product struct {
	Name     string  // unique product name (lowercase)
	Latest   Version // latest version in semantic version format
	Releases []Release
}

type Release struct {
	ShortVersion string  // only major and minor
	Version      Version // complete semantic version
	Variants     []Variant
}

type Version string

func (v Version) IsFull() bool {
	re, err := regexp.Compile("^[0-9]+\\.[0-9]+\\.[0-9]+$")
	return err == nil && re.MatchString(string(v))
}

func (v Version) IsShort() bool {
	re, err := regexp.Compile("^[0-9]+\\.[0-9]+\\.[0-9]+$")
	return err == nil && re.MatchString(string(v))
}

func (v Version) IsValid() bool {
	return v.IsShort() || v.IsFull()
}

type VariantType string

const (
	HTML VariantType = "html"
	PDF  VariantType = "pdf"
	TGZ  VariantType = "tgz"
)

func (v VariantType) IsValid() bool {
	return v == HTML || v == PDF || v == TGZ
}

type Variant struct {
	Type  VariantType
	Files []File
}

type Language struct {
}

type File struct {
	Path string // path relative to variant directory

}

func Open(path string) (*Catalog, error) {
	return nil, nil
}

func (c *Catalog) Save() error {
	return nil
}
