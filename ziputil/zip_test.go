package ziputil

import (
	"archive/zip"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestArchiveFixtures(t *testing.T) {
	f, err := ioutil.TempFile("", "spring-testcase-*.zip")
	assert.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	assert.NoError(t, Archive("fixtures", f))

	z, err := zip.OpenReader(f.Name())
	assert.Equal(t, 2, len(z.File))
}

func TestExtractZipFile(t *testing.T) {
	dirname, err := ioutil.TempDir("", "spring-testcase-*")
	assert.NoError(t, err)
	defer os.RemoveAll(dirname)

	assert.NoError(t, Extract("fixtures.zip", dirname, ""))

	_, err = os.Stat(path.Join(dirname, "photos", "my_photos.json"))
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dirname, "posts", "some_posts.json"))
	assert.NoError(t, err)
}

func TestExtractZipFileWithFilter(t *testing.T) {
	dirname, err := ioutil.TempDir("", "spring-testcase-*")
	assert.NoError(t, err)
	defer os.RemoveAll(dirname)

	assert.NoError(t, Extract("fixtures.zip", dirname, "photos"))

	_, err = os.Stat(path.Join(dirname, "photos", "my_photos.json"))
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dirname, "posts", "some_posts.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestSaveZipFile(t *testing.T) {
	dirname, err := ioutil.TempDir("", "spring-testcase-*")
	assert.NoError(t, err)
	defer os.RemoveAll(dirname)

	z, err := zip.OpenReader("fixtures.zip")
	assert.NoError(t, err)

	savedFiles := []string{}
	for _, file := range z.File {
		savedFiles = append(savedFiles, file.Name)
		assert.NoError(t, saveZipFile(dirname, file))
	}

	for _, filename := range savedFiles {
		_, err := os.Stat(path.Join(dirname, filename))
		assert.NoError(t, err)
	}
}

func TestFileWithin(t *testing.T) {
	assert.True(t, within("posts", "posts/my_post.json"))
	assert.True(t, within("posts", "posts/whatever/my_post.json"))
	assert.True(t, within("posts/whatever", "posts/whatever/my_post.json"))
	assert.False(t, within("posts", "/posts/my_post.json")) // path should not be absolute
	assert.False(t, within("posts", "messages/my_post.json"))
}
