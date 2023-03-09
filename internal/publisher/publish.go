package publisher

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	"cpqd.com.br/alfred/internal/catalog"
)

type PublishParams struct {
	Name     string
	Version  catalog.Version // semantic version
	Language string
	Type     catalog.VariantType
}

func (p PublishParams) IsValid() bool {
	re, err := regexp.Compile("^[a-z][a-z_-]+$")
	if err != nil || !re.MatchString(p.Name) {
		return false
	}
	if p.Language != "pt" && p.Language != "en" && p.Language != "es" {
		return false
	}
	return p.Type.IsValid() && p.Version.IsFull()

}

func Publish(root string, info PublishParams, input io.Reader) error {
	json_path := path.Join(root, info.Name, string(info.Type))
	data_path := path.Join(root, info.Name, string(info.Type), string(info.Version), info.Language)

	err := os.MkdirAll(data_path, 0755)
	if err != nil {
		return err
	}

	fmt.Println(json_path)
	fmt.Println(data_path)
	return extract(data_path, input, info.Type)
}

func extract(dest string, gzipStream io.Reader, variant catalog.VariantType) error {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		return fmt.Errorf("ExtractTarGz: NewReader failed: %s", err.Error())
	}

	tarReader := tar.NewReader(uncompressedStream)

	for true {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("ExtractTarGz: Next() failed: %s", err.Error())
		}

		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg {
			return fmt.Errorf("ExtractTarGz: uknown type: %d in %s", header.Typeflag, header.Name)
		}

		// remove unused parts of the path
		prefix := string(variant) + "/"
		if !strings.HasPrefix(header.Name, prefix) {
			continue
		}
		npath := header.Name[len(prefix):]
		if strings.HasPrefix(npath, ".") {
			continue
		}

		if header.Typeflag == tar.TypeDir {
			if err := os.Mkdir(path.Join(dest, npath), 0755); err != nil {
				//return fmt.Errorf("ExtractTarGz: Mkdir() failed: %s", err.Error())
			}
		} else {
			fmt.Printf("  Inflating %s\n", npath)
			outFile, err := os.Create(path.Join(dest, npath))
			if err != nil {
				return fmt.Errorf("ExtractTarGz: Create() failed: %s", err.Error())
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("ExtractTarGz: Copy() failed: %s", err.Error())
			}
			outFile.Close()

			// check if we have a index file not completely in lower case
			name := path.Base(npath)
			if strings.ToLower(name) == "index.html" && name != "index.html" {
				lpath := path.Join(path.Dir(npath), "index.html")
				fmt.Printf("  Copying %s to %s\n", npath, lpath)
				copy_file(path.Join(dest, npath), path.Join(dest, lpath))
			}
		}
	}
	return nil
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
