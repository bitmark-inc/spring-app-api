package facebook

import (
	"archive/zip"
	"bytes"
	"net/http"
)

func IsValidArchiveFile(fileBytes []byte) bool {
	switch http.DetectContentType(fileBytes) {
	case "application/zip":
		requiredDir := map[string]struct{}{
			"photos_and_videos/": {},
			"posts/":             {},
			"friends/":           {},
		}
		z, err := zip.NewReader(bytes.NewReader(fileBytes), int64(len(fileBytes)))
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
