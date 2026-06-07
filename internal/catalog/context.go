package catalog

import (
	"brunexgeek/alfred/internal/extra"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path"
)

const filePermissions = 0644

type Context struct {
	PageTitle string      // suggested title for the page based on the 'PageType', after substitution
	PageType  string      // page type; possible values are 'TypeEnvironment', 'TypeProduct', 'TypeLanguage' and 'TypeVersion'
	Strings   StringMap   // object to perform substitutions; uses values from 'environment.strings'
	Items     []*MenuItem // array of items in this level
}

type MenuItem struct {
	Id         string // unique ID
	SortableId string // normalized ID to enable sorting
	Title      string // same as 'Id' or substitution
	Type       string // item type; possible values are 'TypeProduct', 'TypeLanguage', 'TypeVersion' and 'TypeFormat'
}

func generateIndexPage(context *Context, basePath string, tpath string) error {
	log := extra.GetDefaultLog()
	fpath := path.Join(basePath, "index.html")

	output, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePermissions)
	if err != nil {
		log.Error(err)
		return err
	}
	defer output.Close()

	if len(tpath) > 0 {
		t, err := template.ParseFiles(tpath)
		if err == nil {
			log.Debugf("Updating index at '%s' with template '%s'\n", fpath, path.Base(tpath))
			err = t.Execute(output, context)
			if err != nil {
				log.Error(err)
			}
			return err
		}
		log.Warn(err)
	}

	log.Debugf("Updating index at '%s'\n", fpath)

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
	log := extra.GetDefaultLog()
	data, err := json.Marshal(context.Items)

	fpath := path.Join(basePath, "metadata.json")
	log.Tracef("Writing metadata to '%s'\n", fpath)
	file, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePermissions)
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
