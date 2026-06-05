package catalog

import (
	langpkg "brunexgeek/alfred/internal/language"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
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
	Id        string                                  `json:"product"` // unique product name (lowercase)
	Tainted   bool                                    `json:"-"`
	Path      string                                  `json:"-"`
	Url       string                                  `json:"url"`
	Languages map[langpkg.LanguageCode]*LanguageEntry `json:"languages"` // indexed by language code
}

type LanguageEntry struct {
	Language langpkg.LanguageCode     `json:"language"`
	Tainted  bool                     `json:"-"`
	Path     string                   `json:"-"`
	Url      string                   `json:"url"`
	Latest   Version                  `json:"latest"`
	Versions map[string]*VersionEntry `json:"versions"` // indexed by semantic version
}

type VersionEntry struct {
	Version Version                     `json:"version"`
	Tainted bool                        `json:"-"`
	Path    string                      `json:"-"`
	Url     string                      `json:"url"`
	Formats map[FormatCode]*FormatEntry `json:"formats"` // indexed by format code
}

type FormatEntry struct {
	Format  FormatCode `json:"format"`
	Path    string     `json:"-"`
	Url     string     `json:"url"`
	RelPath string     `json:"path"`
}

type Publication struct {
	Product  string               `json:"prod"` // unique product name (lowercase)
	Version  Version              `json:"ver"`  // complete semantic version
	Format   FormatCode           `json:"fmt"`
	Language langpkg.LanguageCode `json:"lang"`
	Date     time.Time            `json:"date"`
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
	if !p.Version.HasVersion() {
		return fmt.Errorf("missing version number")
	}
	return nil
}

const RE_PRODUCT_NAME = "([a-z][a-z0-9_]{0,31})"
const RE_FORMAT = "(" + HTML + "|" + PDF + "|" + TGZ + ")"

var name_re, _ = regexp.Compile("^" + RE_PRODUCT_NAME + "$")

func (v FormatCode) IsValid() bool {
	return v == HTML || v == PDF || v == TGZ
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

func (c *Catalog) AddPublication(pub *Publication) error {
	err := pub.Validate()
	if err != nil {
		return err
	}
	ok := false
	var product *ProductEntry
	var language *LanguageEntry
	var version *VersionEntry

	root := path.Join(c.Environment.Path, pub.Product)
	url := strings.Join([]string{".", pub.Product}, "/")
	if product, ok = c.Products[pub.Product]; !ok {
		product = &ProductEntry{pub.Product, true, root, url, make(map[langpkg.LanguageCode]*LanguageEntry, 0)}
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
	if pub.Version.IsNewer(language.Latest) {
		language.Latest = pub.Version
	}

	root = path.Join(root, string(pub.Format))
	url = strings.Join([]string{url, string(pub.Format)}, "/")
	if _, ok = version.Formats[pub.Format]; !ok {
		format := &FormatEntry{pub.Format, root, url, pub.DataPath()}
		version.Formats[pub.Format] = format
		version.Tainted = true
	}

	return nil
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
			language := langpkg.LanguageCode(entry.Name())
			if !entry.IsDir() || !language.IsValid() {
				continue
			}

			// for each version
			root := path.Join(root, entry.Name())
			for _, entry := range read_directory(root) {
				version, err := ParseVersion(entry.Name())
				if !entry.IsDir() || err != nil || !version.HasVersion() {
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
		stringMap := StringMap{&c.Environment.Strings}
		{
			context := &Context{
				Root:     root,
				Strings:  stringMap,
				PageType: "product",
			}
			err := generateIndexPage(context, c.Environment.Path, c.Environment.Templates.Products)
			if err != nil {
				return err
			}
		}

		for _, product := range root.Children {
			productPath := c.Environment.Path + "/" + product.Name
			if product.Tainted {
				context := &Context{
					Root:     product,
					Strings:  stringMap,
					PageType: "language",
				}
				err := generateIndexPage(context, productPath, c.Environment.Templates.Languages)
				if err != nil {
					return err
				}
			}

			for _, language := range product.Children {
				languagePath := productPath + "/" + language.Name
				if language.Tainted {
					context := &Context{
						Root:     language,
						Strings:  stringMap,
						PageType: "version",
					}
					err := generateIndexPage(context, languagePath, c.Environment.Templates.Versions)
					if err != nil {
						return err
					}
				}

				for _, version := range language.Children {
					if version.Tainted {
						versionPath := languagePath + "/" + version.Name
						context := &Context{
							Root:     version,
							Strings:  stringMap,
							PageType: "format",
						}
						err := generateIndexPage(context, versionPath, c.Environment.Templates.Formats)
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
		if product.Tainted {
			err := generateMetadata(product, c.Environment.Path)
			if err != nil {
				return err
			}
			product.Tainted = false
		}
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

type StringMap struct {
	table *map[string]string
}

func (t *StringMap) Get(key string) string {
	if output, ok := (*t.table)[key]; ok {
		return output
	}
	return key
}

type Context struct {
	Root     *MenuItem
	PageType string
	Strings  StringMap
}

func generateIndexPage(context *Context, basePath string, tpath string) error {
	fpath := path.Join(basePath, "index.html")
	output, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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
	title := context.Strings.Get(context.PageType)
	output.WriteString(fmt.Sprintf(PAGE_HEADER, title, title))

	for _, item := range context.Root.Children {
		const PAGE_ITEM = "<li><a href='%s'>%s</a></li>"
		output.WriteString(fmt.Sprintf(PAGE_ITEM, item.Name, item.TranslatedName))
	}

	const PAGE_FOOTER = `</ul></body></html>`
	output.WriteString(PAGE_FOOTER)

	return nil
}

func createCatalogIndex(c *Catalog) ([]*MenuItem, error) {
	output := make([]*MenuItem, 0)
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
		output = append(output, item)
	}

	// sort products in ascending order
	slices.SortStableFunc(output, func(a, b *MenuItem) int {
		return strings.Compare(a.TranslatedName, b.TranslatedName)
	})

	return output, nil
}

func createLanguageIndex(c *Catalog, p *ProductEntry) ([]*MenuItem, error) {
	output := make([]*MenuItem, 0)
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
		output = append(output, item)
	}

	// sort languages in ascending order
	slices.SortStableFunc(output, func(a, b *MenuItem) int {
		return strings.Compare(a.TranslatedName, b.TranslatedName)
	})

	return output, nil
}

func createVersionIndex(c *Catalog, l *LanguageEntry) ([]*MenuItem, error) {
	output := make([]*MenuItem, 0)
	for _, version := range l.Versions {

		children, err := createFormatIndex(c, version)
		if err != nil {
			return nil, err
		}

		item := &MenuItem{
			TranslatedName: version.Version.ToSortableString(),
			Name:           version.Version.ToString(),
			Children:       children,
			ChildrenType:   "format",
			Tainted:        version.Tainted,
			Type:           "version",
		}
		output = append(output, item)
	}

	// sort versions in descending order
	slices.SortStableFunc(output, func(a, b *MenuItem) int {
		return strings.Compare(a.TranslatedName, b.TranslatedName) * -1
	})

	for _, item := range output {
		item.TranslatedName = item.Name
	}

	return output, nil
}

func createFormatIndex(c *Catalog, v *VersionEntry) ([]*MenuItem, error) {
	output := make([]*MenuItem, 0)
	for _, format := range v.Formats {
		item := &MenuItem{
			TranslatedName: c.Environment.Translate(string(format.Format)),
			Name:           string(format.Format),
			Type:           "format",
		}
		output = append(output, item)
	}

	// sort format in ascending order
	slices.SortStableFunc(output, func(a, b *MenuItem) int {
		return strings.Compare(a.TranslatedName, b.TranslatedName)
	})

	return output, nil
}

func generateMetadata(product *ProductEntry, basePath string) error {
	data, err := json.Marshal(product)

	fpath := path.Join(basePath, product.Id, "metadata.json")
	fmt.Printf("Writing metadata to '%s'\n", fpath)
	file, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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

func (e *Environment) Translate(expr string) string {
	output, ok := e.Strings[expr]
	if !ok {
		return expr
	}
	return output
}
