package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

var name_re, _ = regexp.Compile(`^([a-z][a-z0-9_\-]{0,31})$`)

type Catalog struct {
	mutex       sync.Mutex               `json:"-"`
	Tainted     bool                     `json:"-"`
	Environment *Environment             `json:"-"`
	Products    map[string]*ProductEntry `json:"products"`
}

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

type ProductEntry struct {
	Id        string                          `json:"product"` // unique product name (lowercase)
	Tainted   bool                            `json:"-"`
	Path      string                          `json:"-"`
	Url       string                          `json:"url"`
	Languages map[LanguageCode]*LanguageEntry `json:"languages"` // indexed by language code
}

type LanguageEntry struct {
	Language LanguageCode             `json:"language"`
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
	Product  string       `json:"prod"` // unique product name (lowercase)
	Version  Version      `json:"ver"`  // complete semantic version
	Format   FormatCode   `json:"fmt"`
	Language LanguageCode `json:"lang"`
	Date     time.Time    `json:"date"`
}

func NewCatalog(env *Environment) *Catalog {
	return &Catalog{
		Tainted:     true,
		Environment: env,
		Products:    make(map[string]*ProductEntry),
	}
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

func (c *Catalog) Serialize() ([]byte, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

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
	c.mutex.Lock()
	defer c.mutex.Unlock()

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

func (c *Catalog) UpdateWebIndices() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	toc, err := createCatalogIndex(c)
	if err != nil {
		return err
	}
	root := &MenuItem{
		Name:           "catalog",
		TranslatedName: c.Environment.Translate("catalog"),
		Children:       toc,
		ChildrenType:   "product",
		Type:           "catalog",
	}

	stringMap := StringMap{&c.Environment.Strings}
	if c.Tainted {
		context := &Context{
			Root:     root,
			Strings:  stringMap,
			PageType: "productList",
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
				PageType: "languageList",
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
					PageType: "versionList",
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
						PageType: "formatList",
					}
					err := generateIndexPage(context, versionPath, c.Environment.Templates.Formats)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	// untaint everything and generate JSON metadata
	err = nil
	c.Tainted = false
	for _, product := range c.Products {
		if product.Tainted {
			if err == nil {
				err = generateMetadata(product, c.Environment.Path)
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

	return err
}

func (e *Environment) Translate(expr string) string {
	output, ok := e.Strings[expr]
	if !ok {
		return expr
	}
	return output
}

func read_directory(root string) []os.DirEntry {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []os.DirEntry{}
	}
	return entries
}
