package facebook

import (
	"archive/zip"
	"net/http"
	"os"

	log "github.com/sirupsen/logrus"
)

func IsValidArchiveFile(filename string) bool {
	logEntity := log.WithField("prefix", "validate_archive")

	file, err := os.Open(filename)
	if err != nil {
		logEntity.Error(err)
		return false
	}
	defer file.Close()

	fs, err := file.Stat()
	if err != nil {
		logEntity.Error(err)
		return false
	}

	fileHead := make([]byte, 512)
	if _, err := file.Read(fileHead); err != nil {
		logEntity.Error(err)
		return false
	}
	logEntity.WithField("head", fileHead).Debug("extract content type")

	if _, err := file.Seek(0, 0); err != nil {
		return false
	}
	switch http.DetectContentType(fileHead) {
	case "application/zip":
		requiredDir := map[string]struct{}{
			"photos_and_videos/": {},
			"posts/":             {},
			"friends/":           {},
		}

		z, err := zip.NewReader(file, fs.Size())
		if err != nil {
			return false
		}

		for _, f := range z.File {
			if f.Mode().IsDir() {
				if _, ok := requiredDir[f.Name]; ok {
					delete(requiredDir, f.Name)
				}
			}
		}

		if len(requiredDir) != 0 {
			return false
		}

		return true
	default:
		return false
	}
}
