package publisher

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"cpqd.com.br/alfred/internal/catalog"
)

type Publisher struct {
	Catalog *catalog.Catalog
	Path    string // path to the catalog in disk
}

type Summary struct {
	Count int   `json:"count"` // number of published files
	Total int64 `json:"total"` // amount of disk space used to store the files
}

func NewPublisher(fpath string) (*Publisher, error) {
	pub := Publisher{Path: fpath}
	var err error
	pub.Catalog, err = catalog.Open(fpath)
	if err != nil {
		return nil, err
	}
	return &pub, nil
}

func (p *Publisher) Save() error {
	return p.Catalog.Save(p.Path)
}

func Publish(root string, pub *catalog.Publication, input io.Reader) (*Summary, error) {
	data_path := path.Join(root, pub.DataPath())

	err := os.MkdirAll(data_path, 0755)
	if err != nil {
		return nil, err
	}

	summary, err := extract(data_path, input, pub.Format)
	if err != nil {
		return nil, err
	}
	return summary, nil
}

func extract(dest string, gzipStream io.Reader, format catalog.FormatType) (*Summary, error) {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		return nil, fmt.Errorf("Unable to open gzip stream %s", err.Error())
	}

	tarReader := tar.NewReader(uncompressedStream)
	summary := Summary{}

	for true {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("Error parsing TAR: %s", err.Error())
		}
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("Unsupported TAR entry %d of %s", header.Typeflag, header.Name)
		}

		// files must be prefixed with the specified format (e.g. "html/" for "html")
		// files with any other prefix will be ignored
		prefix := string(format) + "/"
		if !strings.HasPrefix(header.Name, prefix) {
			continue
		}
		npath := header.Name[len(prefix):]
		// ignore hidden files/directories
		if strings.HasPrefix(npath, ".") {
			continue
		}

		if header.Typeflag == tar.TypeDir {
			if err := os.Mkdir(path.Join(dest, npath), 0755); err != nil {
				//return fmt.Errorf("ExtractTarGz: Mkdir() failed: %s", err.Error())
			}
		} else {
			//fmt.Printf("  Inflating %s\n", npath)
			outFile, err := os.Create(path.Join(dest, npath))
			if err != nil {
				return nil, fmt.Errorf("Unable to create file %s: %s", npath, err.Error())
			}
			size, err := io.Copy(outFile, tarReader)
			if err != nil {
				outFile.Close()
				return nil, fmt.Errorf("Unable to copy data to %s: %s", npath, err.Error())
			}
			outFile.Close()
			summary.Count++
			summary.Total += size

			// check if we have a index file not completely in lower case
			name := path.Base(npath)
			if strings.ToLower(name) == "index.html" && name != "index.html" {
				lpath := path.Join(path.Dir(npath), "index.html")
				//fmt.Printf("  Copying %s to %s\n", npath, lpath)
				copy_file(path.Join(dest, npath), path.Join(dest, lpath))
			}
		}
	}
	return &summary, nil
}

func copy_file(in, out string) (int64, error) {
	i, e := os.Open(in)
	if e != nil {
		return 0, e
	}
	defer i.Close()
	o, e := os.Create(out)
	if e != nil {
		return 0, e
	}
	defer o.Close()
	return o.ReadFrom(i)
}
