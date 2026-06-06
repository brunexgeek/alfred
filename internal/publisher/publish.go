package publisher

import (
	"archive/tar"
	"brunexgeek/alfred/internal/catalog"
	"brunexgeek/alfred/internal/extra"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const MAX_PAYLOAD = 25 * 1024 * 1024
const PERMISSIONS = 0744

var log extra.Logger = *extra.NewLogger(extra.DebugLevel)

type Publisher struct {
	Path string // path to the catalog in disk
}

type Summary struct {
	Count int   `json:"count"` // number of published files
	Size  int64 `json:"size"`  // amount of disk space used to store the files (bytes)
}

func Publish(root string, pub *catalog.Publication, input io.Reader) (*Summary, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	data_path := path.Join(root, pub.DataPath())

	err := os.MkdirAll(data_path, PERMISSIONS)
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
		summary, err = extract(data_path, ustream)
		if err != nil {
			return nil, err
		}
	} else {
		// just copy for other formats ('pdf' and 'tgz')

		fname := path.Join(data_path, fmt.Sprintf("%s-%s-%s.",
			pub.Product, pub.Version.ToString(), pub.Language))
		switch pub.Format {
		case catalog.PDF:
			fname += "pdf"
		case catalog.TGZ:
			fname += "tar.gz"
		default:
			return nil, fmt.Errorf("unsupported format")
		}

		summary, err = save_file(fname, input)
		if err != nil {
			return nil, err
		}
	}

	return summary, nil
}

func save_file(fpath string, input io.Reader) (*Summary, error) {
	output, err := os.OpenFile(fpath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, PERMISSIONS)
	if err != nil {
		return nil, fmt.Errorf("unable to create file %s: %s", fpath, err.Error())
	}
	size, err := io.Copy(output, input)
	output.Close()
	if err != nil {
		return nil, fmt.Errorf("unable to copy data to %s: %s", fpath, err.Error())
	}

	return &Summary{Count: 1, Size: size}, nil
}

// Inflate gzip content to memory
func inflate(gzstream io.Reader) (*bytes.Reader, error) {
	stream, err := gzip.NewReader(gzstream)
	if err != nil {
		return nil, fmt.Errorf("unable to open gzip stream %s", err.Error())
	}

	data, err := extra.ReadAll(stream, MAX_PAYLOAD)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(data), nil
}

// Validate a TAR package content
func validate(stream *bytes.Reader) error {
	stream.Seek(0, io.SeekStart)
	tarReader := tar.NewReader(stream)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("error parsing TAR: %s", err.Error())
		}
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg {
			return fmt.Errorf("unsupported TAR entry %d of %s", header.Typeflag, header.Name)
		}

		// check for hidden files/directories
		if strings.HasPrefix(header.Name, ".") {
			return fmt.Errorf("payload must not contain hidden files")
		}
	}
	stream.Seek(0, io.SeekStart)
	return nil
}

func extract(dest string, stream *bytes.Reader) (*Summary, error) {
	err := validate(stream)
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
			return nil, fmt.Errorf("error parsing TAR: %s", err.Error())
		}

		npath := header.Name
		// ignore hidden files/directories
		if strings.HasPrefix(npath, ".") {
			continue
		}

		if header.Typeflag == tar.TypeDir {
			target := path.Join(dest, npath)
			if err := os.Mkdir(target, PERMISSIONS); err != nil {
				return nil, fmt.Errorf("unable to create directory '%s': %s", target, err.Error())
			}
		} else {
			log.Debugf("  Inflating %s\n", npath)
			outFile, err := os.OpenFile(path.Join(dest, npath), os.O_WRONLY|os.O_TRUNC|os.O_CREATE, PERMISSIONS)
			if err != nil {
				return nil, fmt.Errorf("unable to create file %s: %s", npath, err.Error())
			}
			size, err := io.Copy(outFile, tarReader)
			outFile.Close()
			if err != nil {
				return nil, fmt.Errorf("unable to copy data to %s: %s", npath, err.Error())
			}
			summary.Count++
			summary.Size += size

			// if we have an index file not completely in lower case
			// we should copy the file (do not use links to avoid security
			// problems while serving with nginx or httpd)
			name := path.Base(npath)
			if strings.ToLower(name) == "index.html" && name != "index.html" {
				lpath := path.Join(path.Dir(npath), "index.html")
				log.Debugf("  Copying %s to %s\n", npath, lpath)
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
