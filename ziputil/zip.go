package ziputil

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Archive(dirname string, w io.Writer) error {
	archive := zip.NewWriter(w)
	defer archive.Close()

	return filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		header.Name = strings.TrimPrefix(path, dirname)
		header.Method = zip.Deflate

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}

		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})

}

func Extract(source, destination, filter string) error {
	r, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if filter != "" && !within(filter, f.Name) {
			continue
		}

		if err := saveZipFile(destination, f); err != nil {
			return err
		}
	}
	return nil
}

// saveZipFile saves a file of zipfile to destination
func saveZipFile(destination string, file *zip.File) error {
	fpath := filepath.Join(destination, file.Name)

	// check for Zip Slip vulnerability: http://bit.ly/2MsjAWE
	if !strings.HasPrefix(fpath, filepath.Clean(destination)+string(os.PathSeparator)) {
		return fmt.Errorf("zip slip detected")
	}

	if file.FileInfo().IsDir() {
		if err := os.MkdirAll(fpath, os.ModePerm); err != nil {
			return err
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
		return err
	}

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
	if err != nil {
		return err
	}
	defer outFile.Close()

	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(outFile, rc)
	return err
}

// within returns true if a path is relatively inside a parent directory
func within(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}

	return !strings.Contains(rel, "..")
}
