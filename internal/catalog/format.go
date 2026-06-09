package catalog

type FormatCode string

const (
	HTML FormatCode = "html"
	PDF  FormatCode = "pdf"
	TGZ  FormatCode = "tgz"
)

func (v FormatCode) IsValid() bool {
	return v == HTML || v == PDF || v == TGZ
}

func IsValidFormat(value string) bool {
	return FormatCode(value).IsValid()
}
