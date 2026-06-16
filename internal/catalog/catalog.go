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
	root       Entry
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
	TypeEnvironment EntryType = "environment"
	TypeProduct     EntryType = "product"
	TypeLanguage    EntryType = "language"
	TypeVersion     EntryType = "version"
	TypeFormat      EntryType = "format"
)

type Entry struct {
	Parent   *Entry
	Id       string // original ID
	NormalId string // normalized ID for sorting
	Title    string // ID after substitutions or custom title
	Path     string // absolute path to this entry in the disk
	Children map[string]*Entry
	Tainted  bool // was this entry changed since last index generation?
	Type     EntryType
}

type Publication struct {
	Product  string  // unique product name (lowercase)
	Version  Version // complete semantic version
	Format   FormatCode
	Language LanguageCode
	Date     time.Time
}

func NewEnvironment(params *Parameters) *Environment {
	return &Environment{
		Parameters: params,
		root: Entry{
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
	if item.NormalId == "" {
		item.NormalId = item.Id
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

	if product, ok = c.root.Children[pub.Product]; !ok {
		title := c.Parameters.Translate(pub.Product)
		product = &Entry{
			Id:       pub.Product,
			NormalId: strings.ToLower(title),
			Title:    title,
			Type:     TypeProduct,
		}
		c.root.AppendChild(product)
	}

	if language, ok = product.Children[string(pub.Language)]; !ok {
		id := "language_" + string(pub.Language)
		title, ok := c.Parameters.Strings[id]
		if !ok {
			title = pub.Language.GetName()
		}
		language = &Entry{
			Id:       string(pub.Language),
			NormalId: strings.ToLower(title),
			Title:    title,
			Type:     TypeLanguage,
		}
		product.AppendChild(language)
	}

	if version, ok = language.Children[pub.Version.ToString()]; !ok {
		version = &Entry{
			Id:       pub.Version.ToString(),
			NormalId: pub.Version.ToSortableString(),
			Title:    pub.Version.ToString(),
			Type:     TypeVersion,
		}
		language.AppendChild(version)
	}

	if _, ok = version.Children[string(pub.Format)]; !ok {
		format := &Entry{
			Id:       string(pub.Format),
			NormalId: string(pub.Format),
			Title:    c.Parameters.Translate(string(pub.Format)),
			Type:     TypeFormat,
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

func (c *Environment) DeletePublication(resource []string) (*Entry, error) {
	if len(resource) < 2 || len(resource) > 5 {
		return nil, fmt.Errorf("incomplete resource name")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	entry := &c.root
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

func (c *Environment) GetPublication(resource []string) ([]byte, error) {
	if len(resource) < 1 || len(resource) > 5 {
		return nil, fmt.Errorf("incomplete resource name")
	}
	if len(resource) > 4 {
		return nil, fmt.Errorf("information not available")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	entry := &c.root
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

	input, err := os.OpenFile(path.Join(entry.Path, "metadata.json"), os.O_RDONLY, filePermissions)
	if err != nil {
		return nil, err
	}
	content, err := extra.ReadAll(input, 32*1024)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func treefy(entry *Entry, prevPath string, prevNormalPath string) *MenuItem {
	var childPath string
	var childNormalPath string

	root := &MenuItem{
		Path:       prevPath,
		NormalPath: prevNormalPath,
		Title:      entry.Title,
		Type:       string(entry.Type),
		Children:   make([]*MenuItem, 0, len(entry.Children)),
	}
	for _, child := range entry.Children {
		if prevPath != "" {
			childPath = fmt.Sprintf("%s/%s", prevPath, child.Id)
			childNormalPath = fmt.Sprintf("%s/%s", prevNormalPath, child.NormalId)
		} else {
			childPath = child.Id
			childNormalPath = child.NormalId
		}
		root.Children = append(root.Children, treefy(child, childPath, childNormalPath))
	}
	// sort in ascending order
	slices.SortStableFunc(root.Children, func(a, b *MenuItem) int {
		return strings.Compare(a.NormalPath, b.NormalPath)
	})
	return root
}

func generateTreeItems(entry *Entry) []*MenuItem {
	items := treefy(entry, "", "").Children
	return items
}

func (c *Environment) updateWebIndex(entry *Entry) error {
	if entry.Tainted {
		context := Context{
			Items:     generateTreeItems(entry),
			PageTitle: c.Parameters.Translate("page_" + string(entry.Type)),
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
	return c.updateWebIndex(&c.root)
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
