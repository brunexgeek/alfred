package catalog

import (
	"brunexgeek/alfred/internal/extra"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var name_re, _ = regexp.Compile(`^([a-z][a-z0-9_\-]{0,31})$`)

type Environment struct {
	mutex      sync.Mutex
	Parameters *Parameters
	Root       Entry
}

type Parameters struct {
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

type EntryType string

const (
	TypeEnvironment EntryType = "TypeEnvironment"
	TypeProduct     EntryType = "TypeProduct"
	TypeLanguage    EntryType = "TypeLanguage"
	TypeVersion     EntryType = "TypeVersion"
	TypeFormat      EntryType = "TypeFormat"
)

type Entry struct {
	Parent     *Entry
	Id         string // original ID
	SortableId string // normalized ID to enable sorting
	Title      string // ID after substitutions or custom title
	Path       string // absolute path to this entry in the disk
	Children   map[string]*Entry
	Tainted    bool // was this entry changed since last index generation?
	Type       EntryType
}

type Publication struct {
	Product  string       `json:"prod"` // unique product name (lowercase)
	Version  Version      `json:"ver"`  // complete semantic version
	Format   FormatCode   `json:"fmt"`
	Language LanguageCode `json:"lang"`
	Date     time.Time    `json:"date"`
}

func NewEnvironment(params *Parameters) *Environment {
	return &Environment{
		Parameters: params,
		Root: Entry{
			Path:     params.Path,
			Children: make(map[string]*Entry),
			Type:     TypeEnvironment,
			Tainted:  true,
		},
	}
}

func IsValidName(name string) bool {
	return name_re != nil && name_re.MatchString(name)
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

func (p *Publication) DataPath() string {
	return "/" + path.Join(p.Product, string(p.Language), p.Version.ToString(), string(p.Format))
}

func (e *Entry) AppendChild(item *Entry) {
	item.Path = path.Join(e.Path, item.Id)
	item.Parent = e
	if item.Children == nil {
		item.Children = make(map[string]*Entry, 0)
	}
	if item.SortableId == "" {
		item.SortableId = item.Id
	}
	if item.Title == "" {
		item.Title = item.Id
	}
	e.Tainted = true
	e.Children[item.Id] = item
}

func (c *Environment) AddPublication(pub *Publication) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	err := pub.Validate()
	if err != nil {
		return err
	}

	ok := false
	var product *Entry
	var language *Entry
	var version *Entry

	if product, ok = c.Root.Children[pub.Product]; !ok {
		title := c.Parameters.Translate(pub.Product)
		product = &Entry{
			Id:         pub.Product,
			SortableId: strings.ToLower(title),
			Title:      title,
			Type:       TypeProduct,
		}
		c.Root.AppendChild(product)
	}

	if language, ok = product.Children[string(pub.Language)]; !ok {
		id := "language_" + string(pub.Language)
		title, ok := c.Parameters.Strings[id]
		if !ok {
			title = pub.Language.GetName()
		}
		language = &Entry{
			Id:         string(pub.Language),
			SortableId: strings.ToLower(title),
			Title:      title,
			Type:       TypeLanguage,
		}
		product.AppendChild(language)
	}

	if version, ok = language.Children[pub.Version.ToString()]; !ok {
		version = &Entry{
			Id:         pub.Version.ToString(),
			SortableId: pub.Version.ToSortableString(),
			Title:      pub.Version.ToString(),
			Type:       TypeVersion,
		}
		language.AppendChild(version)
	}

	if _, ok = version.Children[string(pub.Format)]; !ok {
		format := &Entry{
			Id:         string(pub.Format),
			SortableId: string(pub.Format),
			Title:      c.Parameters.Translate(string(pub.Format)),
			Type:       TypeFormat,
		}
		version.AppendChild(format)
	}

	return nil
}

func isRemoved(entry os.DirEntry, parentDir string) bool {
	if !entry.IsDir() {
		return false
	}

	marker := filepath.Join(parentDir, entry.Name(), "__remove__")

	info, err := os.Stat(marker)
	if err != nil {
		return false
	}

	return !info.IsDir()
}

func (c *Environment) ScanEnvironment(basePath string) {
	log := extra.GetDefaultLog()
	now := time.Now()

	// for each product
	for _, entry := range read_directory(basePath) {
		product := entry.Name()
		if !entry.IsDir() || !name_re.MatchString(product) || isRemoved(entry, basePath) {
			continue
		}

		// for each language
		root := path.Join(basePath, product)
		for _, entry := range read_directory(root) {
			language := LanguageCode(entry.Name())
			if !entry.IsDir() || !language.IsValid() || isRemoved(entry, root) {
				continue
			}

			// for each version
			root := path.Join(root, entry.Name())
			for _, entry := range read_directory(root) {
				version, err := ParseVersion(entry.Name())
				if !entry.IsDir() || err != nil || !version.HasVersion() || isRemoved(entry, root) {
					continue
				}

				// for each format
				root := path.Join(root, entry.Name())
				for _, entry := range read_directory(root) {
					format := FormatCode(entry.Name())
					if !entry.IsDir() || !format.IsValid() || isRemoved(entry, root) {
						continue
					}

					modified := now
					info, err := entry.Info()
					if err != nil {
						log.Warnf("Unable to retrieve information about '%s'", path.Join(root, entry.Name()))
					} else {
						modified = info.ModTime()
					}

					pub := &Publication{
						Product:  product,
						Version:  version,
						Format:   format,
						Language: language,
						Date:     modified,
					}
					c.AddPublication(pub)
				}
			}
		}
	}
}

func (c *Environment) RemovePublication(resource []string) (*Entry, error) {
	if len(resource) < 2 || len(resource) > 5 {
		return nil, fmt.Errorf("incomplete resource name")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	entry := &c.Root
	ok := false
	for i := 1; i < len(resource); i++ {
		entry, ok = entry.Children[resource[i]]
		if !ok {
			return nil, fmt.Errorf("resource not found")
		}
	}

	// sanity check
	if !strings.HasPrefix(entry.Path, c.Parameters.Path) {
		return nil, fmt.Errorf("resource path inconsistence")
	}
	err := os.RemoveAll(entry.Path)
	if err != nil {
		return nil, err
	}
	delete(entry.Parent.Children, entry.Id)
	entry.Parent.Tainted = true

	return entry, nil
}

func (c *Environment) updateWebIndex(entry *Entry) error {
	if entry.Tainted {
		items := make([]*MenuItem, 0, len(entry.Children))
		for _, child := range entry.Children {
			items = append(items, &MenuItem{
				Id:         child.Id,
				Title:      child.Title,
				SortableId: child.SortableId,
				Type:       string(child.Type),
			})
		}
		// sort products in ascending order
		slices.SortStableFunc(items, func(a, b *MenuItem) int {
			return strings.Compare(a.SortableId, b.SortableId)
		})

		context := Context{
			Items:     items,
			PageTitle: c.Parameters.Translate(string(entry.Type)),
			PageType:  string(entry.Type),
			Strings:   StringMap{&c.Parameters.Strings},
		}

		var tpath string
		switch entry.Type {
		case TypeEnvironment:
			tpath = c.Parameters.Templates.Products
		case TypeProduct:
			tpath = c.Parameters.Templates.Languages
		case TypeLanguage:
			tpath = c.Parameters.Templates.Versions
		case TypeVersion:
			tpath = c.Parameters.Templates.Formats
		}

		err := generateIndexPage(&context, entry.Path, tpath)
		if err != nil {
			return err
		}
		err = generateMetadata(&context, entry.Path)
		if err != nil {
			return err
		}

		entry.Tainted = false
	}

	for _, child := range entry.Children {
		c.updateWebIndex(child)
	}

	return nil
}

func (c *Environment) UpdateWebIndices() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	extra.GetDefaultLog().Infof("Updating indices")
	return c.updateWebIndex(&c.Root)
}

func (p *Parameters) Translate(expr string) string {
	output, ok := p.Strings[expr]
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
