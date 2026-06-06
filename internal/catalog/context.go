package catalog

import (
	"brunexgeek/alfred/internal/extra"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path"
)

const PERMISSIONS = 0644

var log extra.Logger = *extra.NewLogger(extra.DebugLevel)

type MenuItem struct {
	Id         string
	SortableId string
	Title      string
	Type       string
}

type Context struct {
	Items     []*MenuItem
	PageTitle string
	PageType  string
	Strings   StringMap
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
	output.WriteString(fmt.Sprintf(PAGE_HEADER, context.PageTitle, context.PageTitle))

	for _, item := range context.Items {
		const PAGE_ITEM = "<li><a href='%s'>%s</a></li>"
		output.WriteString(fmt.Sprintf(PAGE_ITEM, item.Id, item.Title))
	}

	const PAGE_FOOTER = `</ul></body></html>`
	output.WriteString(PAGE_FOOTER)

	return nil
}

func generateMetadata(context *Context, basePath string) error {
	data, err := json.Marshal(context.Items)

	fpath := path.Join(basePath, "metadata.json")
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
