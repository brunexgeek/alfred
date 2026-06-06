package catalog

import (
	"brunexgeek/alfred/internal/extra"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path"
	"slices"
	"strings"
)

const PERMISSIONS = 0644

var log extra.Logger = *extra.NewLogger(extra.DebugLevel)

type Context struct {
	Root     *MenuItem
	PageType string
	Strings  StringMap
}

type MenuItem struct {
	TranslatedName string
	Name           string
	Children       []*MenuItem
	ChildrenType   string
	Tainted        bool
	Type           string
}

func generateIndexPage(context *Context, basePath string, tpath string) error {
	fpath := path.Join(basePath, "index.html")
	output, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, PERMISSIONS)
	if err != nil {
		log.Error(err)
		return err
	}
	defer output.Close()

	if len(tpath) > 0 {
		t, err := template.ParseFiles(tpath)
		if err == nil {
			log.Infof("Updating index at '%s' with template '%s'\n", fpath, path.Base(tpath))
			err = t.Execute(output, context)
			if err != nil {
				log.Error(err)
			}
			return err
		}
		log.Warn(err)
	}

	log.Infof("Updating index at '%s'\n", fpath)

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
	log.Infof("Writing metadata to '%s'\n", fpath)
	file, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, PERMISSIONS)
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

type StringMap struct {
	table *map[string]string
}

func (t *StringMap) Get(key string) string {
	if output, ok := (*t.table)[key]; ok {
		return output
	}
	return key
}
