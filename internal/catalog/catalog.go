package catalog

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
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

type Environment struct {
	Name      string            `json:"name"`
	Path      string            `json:"path"`
	Templates Templates         `json:"templates"`
	Strings   map[string]string `json:"strings"`
}

type Templates struct {
	Products  string `json:"products"`
	Languages string `json:"languages"`
	Versions  string `json:"versions"`
	Formats   string `json:"formats"`
}

type Catalog struct {
	Tainted     bool                     `json:"-"`
	Environment *Environment             `json:"-"`
	Products    map[string]*ProductEntry `json:"products"`
}

type ProductEntry struct {
	Id        string                          `json:"id"` // unique product name (lowercase)
	Tainted   bool                            `json:"-"`
	Path      string                          `json:"-"`
	Url       string                          `json:"-"`
	Languages map[LanguageCode]*LanguageEntry `json:"languages"` // indexed by language code
}

type LanguageEntry struct {
	Language LanguageCode             `json:"-"`
	Tainted  bool                     `json:"-"`
	Path     string                   `json:"-"`
	Url      string                   `json:"-"`
	Latest   Version                  `json:"latest"`
	Versions map[string]*VersionEntry `json:"versions"` // indexed by semantic version
}

type VersionEntry struct {
	Version Version                     `json:"-"`
	Tainted bool                        `json:"-"`
	Path    string                      `json:"-"`
	Url     string                      `json:"-"`
	Formats map[FormatCode]*FormatEntry `json:"formats"` // indexed by format code
}

type FormatEntry struct {
	Format  FormatCode `json:"-"`
	Path    string     `json:"-"`
	Url     string     `json:"-"`
	RelPath string     `json:"path"`
}

type Publication struct {
	Product  string       `json:"prod"` // unique product name (lowercase)
	Version  Version      `json:"ver"`  // complete semantic version
	Format   FormatCode   `json:"fmt"`
	Language LanguageCode `json:"lang"`
	Date     time.Time    `json:"date"`
}

func NewCatalog(env *Environment) *Catalog {
	return &Catalog{true, env, make(map[string]*ProductEntry)}
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
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Fix   int    `json:"fix"`
	Tag   string `json:"tag"`
}

const RE_PRODUCT_NAME = "([a-z][a-z0-9_]{0,31})"
const RE_SHORT_VERSION = "([0-9]{1,3}\\.[0-9]{1,3})"
const RE_URL_VERSION = "([0-9]{1,3}\\.[0-9]{1,3}|latest)"
const RE_FORMAT = "(" + HTML + "|" + PDF + "|" + TGZ + ")"
const RE_LANGUAGE = "(" + PT + "|" + ES + "|" + EN + ")"

var name_re, _ = regexp.Compile("^" + RE_PRODUCT_NAME + "$")
var full_re, _ = regexp.Compile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
var short_re, _ = regexp.Compile("^" + RE_SHORT_VERSION + "$")

func ParseVersion(value string) (Version, error) {
	if !IsValid(value) {
		return Version{}, fmt.Errorf("invalid semantic version")
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

func (c *Catalog) AddPublication(pub *Publication) {
	ok := false
	var product *ProductEntry
	var language *LanguageEntry
	var version *VersionEntry

	root := path.Join(c.Environment.Path, pub.Product)
	url := strings.Join([]string{".", pub.Product}, "/")
	if product, ok = c.Products[pub.Product]; !ok {
		product = &ProductEntry{pub.Product, true, root, url, make(map[LanguageCode]*LanguageEntry, 0)}
		c.Products[pub.Product] = product
		c.Tainted = true
	}

	root = path.Join(root, string(pub.Language))
	url = strings.Join([]string{url, string(pub.Language)}, "/")
	if language, ok = product.Languages[pub.Language]; !ok {
		language = &LanguageEntry{pub.Language, true, root, url, pub.Version, make(map[string]*VersionEntry, 0)}
		product.Languages[pub.Language] = language
		product.Tainted = true
	}

	root = path.Join(root, pub.Version.ToString())
	url = strings.Join([]string{url, pub.Version.ToString()}, "/")
	if version, ok = language.Versions[pub.Version.ToString()]; !ok {
		version = &VersionEntry{pub.Version, true, root, url, make(map[FormatCode]*FormatEntry, 0)}
		language.Versions[pub.Version.ToString()] = version
		language.Tainted = true
	}
	// try to update the latest version
	if pub.Version.Newer(language.Latest) {
		language.Latest = pub.Version
	}

	root = path.Join(root, string(pub.Format))
	url = strings.Join([]string{url, string(pub.Format)}, "/")
	if _, ok = version.Formats[pub.Format]; !ok {
		format := &FormatEntry{pub.Format, root, url, pub.DataPath()}
		version.Formats[pub.Format] = format
		version.Tainted = true
	}
}

func read_directory(root string) []os.DirEntry {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []os.DirEntry{}
	}
	return entries
}

func (c *Catalog) ScanEnvironment(root string) {
	// for each product
	for _, entry := range read_directory(root) {
		product := entry.Name()
		if !entry.IsDir() || !name_re.MatchString(product) {
			continue
		}

		// for each language
		root := path.Join(root, product)
		for _, entry := range read_directory(root) {
			language := LanguageCode(entry.Name())
			if !entry.IsDir() || !language.IsValid() {
				continue
			}

			// for each version
			root := path.Join(root, entry.Name())
			for _, entry := range read_directory(root) {
				version, err := ParseVersion(entry.Name())
				if !entry.IsDir() || err != nil {
					continue
				}

				// for each format
				root := path.Join(root, entry.Name())
				for _, entry := range read_directory(root) {
					format := FormatCode(entry.Name())
					if !entry.IsDir() || !format.IsValid() {
						continue
					}

					pub := &Publication{
						Product:  product,
						Version:  version,
						Format:   format,
						Language: language,
						Date:     time.Now(),
					}
					c.AddPublication(pub)
				}
			}
		}
	}
}

type Iterator struct {
	Product  *ProductEntry
	Language *LanguageEntry
	Version  *VersionEntry
	Format   *FormatEntry
}

func (c *Catalog) Iterate(it func(Iterator)) {
	for _, product := range c.Products {
		for _, language := range product.Languages {
			for _, version := range language.Versions {
				for _, format := range version.Formats {
					it(Iterator{product, language, version, format})
				}
			}
		}
	}
}

func (p *ProductEntry) Iterate(it func(Iterator)) {
	for _, language := range p.Languages {
		for _, version := range language.Versions {
			for _, format := range version.Formats {
				it(Iterator{p, language, version, format})
			}
		}
	}
}

func (l *LanguageEntry) Iterate(it func(Iterator)) {
	for _, version := range l.Versions {
		for _, format := range version.Formats {
			it(Iterator{nil, l, version, format})
		}
	}
}

func (v *VersionEntry) Iterate(it func(Iterator)) {
	for _, format := range v.Formats {
		it(Iterator{nil, nil, v, format})
	}
}

func (c *Catalog) UpdateWebIndices() error {
	var root *MenuItem
	if c.Tainted {
		toc, err := createCatalogIndex(c)
		if err != nil {
			return err
		}
		root = &MenuItem{
			Name:           "catalog",
			TranslatedName: c.Environment.Translate("catalog"),
			Children:       toc,
			ChildrenType:   "product",
			Type:           "catalog",
		}
	}

	if root != nil {
		//title := c.Environment.Translate("Products")
		err := generateIndexPage(c, root, c.Environment.Path, c.Environment.Templates.Products)
		if err != nil {
			return err
		}

		for _, product := range root.Children {
			productPath := c.Environment.Path + "/" + product.Name
			if product.Tainted {
				//title := c.Environment.Translate("Languages")
				err := generateIndexPage(c, product, productPath, c.Environment.Templates.Languages)
				if err != nil {
					return err
				}
			}

			for _, language := range product.Children {
				languagePath := productPath + "/" + language.Name
				if language.Tainted {
					//title := c.Environment.Translate("Versions")
					err := generateIndexPage(c, language, languagePath, c.Environment.Templates.Versions)
					if err != nil {
						return err
					}
				}

				for _, version := range language.Children {
					if version.Tainted {
						versionPath := languagePath + "/" + version.Name
						//title := c.Environment.Translate("Formats")
						err := generateIndexPage(c, version, versionPath, c.Environment.Templates.Formats)
						if err != nil {
							return err
						}
					}
				}
			}
		}
	}

	c.Tainted = false
	for _, product := range c.Products {
		product.Tainted = false
		for _, language := range product.Languages {
			language.Tainted = false
			for _, version := range language.Versions {
				version.Tainted = false
			}
		}
	}

	return nil
}

type MenuItem struct {
	TranslatedName string
	Name           string
	Children       []*MenuItem
	ChildrenType   string
	Tainted        bool
	Type           string
}

type Context struct {
	Item    *MenuItem
	Strings *map[string]string
}

func writePage(fpath string, tpath string, context *Context) error {
	output, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		fmt.Print(err.Error())
		return err
	}
	defer output.Close()

	if len(tpath) > 0 {
		t, err := template.ParseFiles(tpath)
		if err == nil {
			fmt.Printf("Updating index at '%s' with template '%s'\n", fpath, tpath)
			err = t.Execute(output, context)
			if err != nil {
				fmt.Println(err)
			}
			return err
		}
		fmt.Println(err)
	}

	fmt.Printf("Updating index at '%s'\n", fpath)

	// fallback to a simple HTML page
	const PAGE_HEADER = `<!DOCTYPE html><html><head><title>%s</title><meta charset="utf-8"></head><body><h1>%s</h1><ul>`
	title := (*context.Strings)[context.Item.ChildrenType]
	output.WriteString(fmt.Sprintf(PAGE_HEADER, title, title))

	for _, item := range context.Item.Children {
		const PAGE_ITEM = "<li><a href='%s'>%s</a></li>"
		output.WriteString(fmt.Sprintf(PAGE_ITEM, item.Name, item.TranslatedName))
	}

	const PAGE_FOOTER = `</ul></body></html>`
	output.WriteString(PAGE_FOOTER)

	return nil
}

func createCatalogIndex(c *Catalog) ([]*MenuItem, error) {
	menu := make([]*MenuItem, 0)
	for _, product := range c.Products {

		children, err := createLanguageIndex(c, product)
		if err != nil {
			return nil, err
		}

		item := &MenuItem{
			TranslatedName: c.Environment.Translate(product.Id),
			Name:           product.Id,
			Children:       children,
			ChildrenType:   "language",
			Tainted:        product.Tainted,
			Type:           "product",
		}
		menu = append(menu, item)
	}
	return menu, nil
}

func createLanguageIndex(c *Catalog, p *ProductEntry) ([]*MenuItem, error) {
	menu := make([]*MenuItem, 0)
	for _, language := range p.Languages {

		children, err := createVersionIndex(c, language)
		if err != nil {
			return nil, err
		}

		item := &MenuItem{
			TranslatedName: c.Environment.Translate(string(language.Language)),
			Name:           string(language.Language),
			Children:       children,
			ChildrenType:   "version",
			Tainted:        language.Tainted,
			Type:           "language",
		}
		menu = append(menu, item)
	}
	return menu, nil
}

func createVersionIndex(c *Catalog, l *LanguageEntry) ([]*MenuItem, error) {
	menu := make([]*MenuItem, 0)
	for _, version := range l.Versions {

		children, err := createFormatIndex(c, version)
		if err != nil {
			return nil, err
		}

		item := &MenuItem{
			TranslatedName: c.Environment.Translate(version.Version.ToString()),
			Name:           version.Version.ToString(),
			Children:       children,
			ChildrenType:   "format",
			Tainted:        version.Tainted,
			Type:           "version",
		}
		menu = append(menu, item)
	}
	return menu, nil
}

func createFormatIndex(c *Catalog, v *VersionEntry) ([]*MenuItem, error) {
	menu := make([]*MenuItem, 0)
	for _, format := range v.Formats {
		item := &MenuItem{
			TranslatedName: c.Environment.Translate(string(format.Format)),
			Name:           string(format.Format),
			Type:           "format",
		}
		menu = append(menu, item)
	}
	return menu, nil
}

func generateIndexPage(c *Catalog, root *MenuItem, basePath string, template string) error {
	context := &Context{
		Item:    root,
		Strings: &c.Environment.Strings,
	}
	filename := path.Join(basePath, "index.html")
	return writePage(filename, template, context)
}

func (e *Environment) Translate(expr string) string {
	output, ok := e.Strings[expr]
	if !ok {
		return expr
	}
	return output
}
