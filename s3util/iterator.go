package s3util

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type DirectoryIterator struct {
	baseDir   string
	filePaths []string
	bucket    string
	keyPrefix string
	next      struct {
		path string
		f    *os.File
	}
	err error
}

// NewDirectoryIterator creates and returns a new BatchUploadIterator
func NewDirectoryIterator(bucket, dir, keyPrefix string) s3manager.BatchUploadIterator {
	paths := []string{}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		// We care only about files, not directories
		if !info.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})

	return &DirectoryIterator{
		baseDir:   filepath.Base(dir),
		filePaths: paths,
		bucket:    bucket,
		keyPrefix: keyPrefix,
	}
}

// Next opens the next file and stops iteration if it fails to open
// a file.
func (iter *DirectoryIterator) Next() bool {
	if len(iter.filePaths) == 0 {
		iter.next.f = nil
		return false
	}

	f, err := os.Open(iter.filePaths[0])
	iter.err = err

	iter.next.f = f
	iter.next.path = iter.filePaths[0]

	iter.filePaths = iter.filePaths[1:]
	return true && iter.Err() == nil
}

// Err returns an error that was set during opening the file
func (iter *DirectoryIterator) Err() error {
	return iter.err
}

// UploadObject returns a BatchUploadObject and sets the After field to
// close the file.
func (iter *DirectoryIterator) UploadObject() s3manager.BatchUploadObject {
	parts := strings.Split(iter.next.path, iter.baseDir)
	key := filepath.Join(iter.keyPrefix, iter.baseDir, parts[1])
	f := iter.next.f
	return s3manager.BatchUploadObject{
		Object: &s3manager.UploadInput{
			Bucket: &iter.bucket,
			Key:    &key,
			Body:   f,
		},
		// After was introduced in version 1.10.7
		After: func() error {
			return f.Close()
		},
	}
}
