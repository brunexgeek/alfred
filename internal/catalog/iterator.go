package catalog

type Iterator struct {
	Product  *ProductEntry
	Language *LanguageEntry
	Version  *VersionEntry
	Format   *FormatEntry
}

func (c *Catalog) Iterate(it func(Iterator)) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

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
