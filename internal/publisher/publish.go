package publisher

import (
	"archive/tar"
	"brunexgeek/alfred/internal/catalog"
	"brunexgeek/alfred/internal/extra"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const directoryPermissions = 0755
const filePermissions = 0644

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

	err := os.MkdirAll(data_path, directoryPermissions)
	if err != nil {
		return nil, err
	}

	var summary *Summary

	// for 'html' we have to inflate the input gzip stream and
	// extract files of the resulting tar package
	if pub.Format == catalog.HTML {
		summary, err = extract(data_path, input)
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
	output, err := os.OpenFile(fpath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, filePermissions)
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

// Validate a TAR package content
func validate(header *tar.Header) error {
	if header == nil {
		return fmt.Errorf("invalid entry")
	}
	if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg {
		return fmt.Errorf("unsupported TAR entry %d of %s", header.Typeflag, header.Name)
	}

	// check for hidden files/directories
	if strings.HasPrefix(header.Name, ".") {
		return fmt.Errorf("payload must not contain hidden files and relative paths")
	}
	return nil
}

func extract(dest string, stream io.Reader) (*Summary, error) {
	// make sure 'dest' ends with a path separator
	if !strings.HasSuffix(dest, string(os.PathSeparator)) {
		dest += string(os.PathSeparator)
	}
	// make sure the destination is a directory
	info, err := os.Stat(dest)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path '%s' is not a directory", dest)
	}

	gzReader, err := gzip.NewReader(stream)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	summary := Summary{}
	log := extra.GetDefaultLog()

	for true {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error parsing TAR: %s", err.Error())
		}
		if err := validate(header); err != nil {
			return nil, err
		}

		// ignore hidden files/directories
		if strings.HasPrefix(header.Name, ".") {
			continue
		}

		targetPath := path.Clean(path.Join(dest, header.Name))
		// 'dest' is guaranteed to end with a path separator
		if !strings.HasPrefix(targetPath, dest) {
			return nil, fmt.Errorf("TAR entry escapes destination path")
		}

		if header.Typeflag == tar.TypeDir {

			if err := os.MkdirAll(targetPath, directoryPermissions); err != nil {
				return nil, fmt.Errorf("unable to create directory '%s': %s", targetPath, err.Error())
			}
		} else {
			log.Tracef("Inflating %s\n", header.Name)
			outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, filePermissions)
			if err != nil {
				return nil, fmt.Errorf("unable to create file %s: %s", header.Name, err.Error())
			}
			size, err := io.Copy(outFile, tarReader)
			outFile.Close()
			if err != nil {
				return nil, fmt.Errorf("unable to copy data to %s: %s", header.Name, err.Error())
			}
			summary.Count++
			summary.Size += size

			// if we have an index file not completely in lower case
			// we should copy the file (do not use links to avoid security
			// problems while serving)
			name := path.Base(targetPath)
			if strings.ToLower(name) == "index.html" && name != "index.html" {
				lpath := path.Join(path.Dir(targetPath), "index.html")
				log.Tracef("Copying %s to %s\n", targetPath, lpath)
				_, err := copyFile(targetPath, lpath)
				if err != nil {
					if !os.IsExist(err) {
						return nil, err
					}
				}
			}
		}
	}
	return &summary, nil
}

// Copy a file to a destination if the destination do not exists yet.
func copyFile(in, out string) (int64, error) {
	i, e := os.Open(in)
	if e != nil {
		return 0, e
	}
	defer i.Close()
	o, e := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePermissions)
	if e != nil {
		return 0, e
	}
	defer o.Close()
	return o.ReadFrom(i)
}
