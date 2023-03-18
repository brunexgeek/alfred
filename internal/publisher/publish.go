package publisher

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"cpqd.com.br/alfred/internal/catalog"
	"cpqd.com.br/alfred/internal/extra"
)

const MAX_PAYLOAD = 15 * 1024 * 1024

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

	var summary *Summary

	// for 'html' we have to inflate the input gzip stream and
	// extract files of the resulting tar package
	if pub.Format == catalog.HTML {
		ustream, err := inflate(input)
		if err != nil {
			return nil, err
		}
		summary, err = extract(data_path, ustream, pub.Format)
		if err != nil {
			return nil, err
		}
	} else {
		// other formats ('pdf' and 'tgz') need just to be copied

		fname := path.Join(data_path, fmt.Sprintf("%s-%s-%s.",
			pub.Product, pub.Version.ToString(), pub.Language))
		switch pub.Format {
		case catalog.PDF:
			fname += "pdf"
		case catalog.TGZ:
			fname += "tar.gz"
		default:
			return nil, fmt.Errorf("Unsupported format")
		}

		summary, err = save_file(fname, input)
		if err != nil {
			return nil, err
		}
	}

	return summary, nil
}

func save_file(fpath string, input io.Reader) (*Summary, error) {
	output, err := os.OpenFile(fpath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0755)
	if err != nil {
		return nil, fmt.Errorf("Unable to create file %s: %s", fpath, err.Error())
	}
	size, err := io.Copy(output, input)
	output.Close()
	if err != nil {
		return nil, fmt.Errorf("Unable to copy data to %s: %s", fpath, err.Error())
	}

	return &Summary{Count: 1, Total: size}, nil
}

// Inflate gzip content to memory
func inflate(gzstream io.Reader) (*bytes.Reader, error) {
	stream, err := gzip.NewReader(gzstream)
	if err != nil {
		return nil, fmt.Errorf("Unable to open gzip stream %s", err.Error())
	}

	data, err := extra.ReadAll(stream, MAX_PAYLOAD)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(data), nil
}

func deflate(ostream io.Writer, istream io.Reader) error {
	gstream := gzip.NewWriter(ostream)
	_, err := io.Copy(gstream, istream)
	if err != nil {
		return err
	}
	return nil
}

func deflate_to_file(fpath string, istream io.Reader) error {
	file, err := os.OpenFile(fpath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0755)
	if err != nil {
		return fmt.Errorf("Unable to create file %s: %s", fpath, err.Error())
	}
	err = deflate(file, istream)
	if err != nil {
		file.Close()
		return fmt.Errorf("Unable to copy data to %s: %s", fpath, err.Error())
	}
	file.Close()
	return nil
}

func validate(stream *bytes.Reader, format catalog.FormatType) error {
	stream.Seek(0, io.SeekStart)
	tarReader := tar.NewReader(stream)

	for true {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("Error parsing TAR: %s", err.Error())
		}
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg {
			return fmt.Errorf("Unsupported TAR entry %d of %s", header.Typeflag, header.Name)
		}

		// files must be prefixed with the specified format (e.g. "html/" for "html")
		// files with any other prefix will fail
		prefix := string(format) + "/"
		if !strings.HasPrefix(header.Name, prefix) {
			return fmt.Errorf("Missing format prefix in one or more files")
		}
		npath := header.Name[len(prefix):]
		// check for hidden files/directories
		if strings.HasPrefix(npath, ".") {
			return fmt.Errorf("Payload must not contain hidden files")
		}
		// check for object inventory
		if strings.HasSuffix(npath, "objects.inv") {
			return fmt.Errorf("Payload must not contain 'object.inv' file")
		}
	}
	stream.Seek(0, io.SeekStart)
	return nil
}

func extract(dest string, stream *bytes.Reader, format catalog.FormatType) (*Summary, error) {
	err := validate(stream, format)
	if err != nil {
		return nil, err
	}

	tarReader := tar.NewReader(stream)
	summary := Summary{}

	for true {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("Error parsing TAR: %s", err.Error())
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
			target := path.Join(dest, npath)
			if err := os.Mkdir(target, 0755); err != nil {
				//return nil, fmt.Errorf("Unable to create directory '%s': %s", target, err.Error())
			}
		} else {
			//fmt.Printf("  Inflating %s\n", npath)
			outFile, err := os.OpenFile(path.Join(dest, npath), os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0755)
			if err != nil {
				return nil, fmt.Errorf("Unable to create file %s: %s", npath, err.Error())
			}
			size, err := io.Copy(outFile, tarReader)
			outFile.Close()
			if err != nil {
				return nil, fmt.Errorf("Unable to copy data to %s: %s", npath, err.Error())
			}
			summary.Count++
			summary.Total += size

			// if we have an index file not completely in lower case
			// we should copy the file (do not use links to avoid security
			// problems while serving with nginx or httpd)
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
