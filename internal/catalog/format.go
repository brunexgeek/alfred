package catalog

type FormatCode string

const (
	HTML FormatCode = "html"
	PDF  FormatCode = "pdf"
	TGZ  FormatCode = "tgz"
)

const RE_FORMAT = "(" + HTML + "|" + PDF + "|" + TGZ + ")"

func (v FormatCode) IsValid() bool {
	return v == HTML || v == PDF || v == TGZ
}
