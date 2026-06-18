package catalog

import (
	"fmt"
	"path"
	"time"
)

type Publication struct {
	Product  string  // unique product name (lowercase)
	Version  Version // complete semantic version
	Format   FormatCode
	Language LanguageCode
	Date     time.Time
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
