package facebook

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

type testCase struct {
	content string
	valid   bool
}

func TestPostsPattern(t *testing.T) {
	cases := map[string]testCase{
		"/tmp/user-a/posts/your_posts_1.json":                          {`[{"timestamp":1578201080,"attachments":[{"data":[{"external_context":{"url":"LINK"}}]}],"data":[{"post":"POST"},{"update_timestamp":1578201080}],"title":"TITLE"}]`, true},
		"/tmp/user-a/posts/your_posts_2.json":                          {`{"key": "value"}`, false},
		"/tmp/user-a/posts/notes.json":                                 {`DOESN'T MATTER`, false},
		"/tmp/user-a/posts/other_people's_posts_to_your_timeline.json": {`DOESN'T MATTER`, false},
	}
	fs := afero.NewMemMapFs()

	// create test files and directories
	fs.MkdirAll("/tmp", 0755)
	for filename, item := range cases {
		afero.WriteFile(fs, filename, []byte(item.content), 0644)
	}

	p := PostsPattern
	filenames, err := p.SelectFiles(fs, "/tmp/user-a/posts")
	assert.Equal(t, []string{"/tmp/user-a/posts/your_posts_1.json", "/tmp/user-a/posts/your_posts_2.json"}, filenames)
	assert.NoError(t, err)

	for _, n := range filenames {
		data, err := afero.ReadFile(fs, n)
		assert.NoError(t, err)

		err = p.Validate(data)
		assert.Equal(t, cases[n].valid, err == nil)
	}

	filenames, err = p.SelectFiles(fs, "/tmp/user-b/posts")
	assert.Empty(t, filenames)
	assert.NoError(t, err)
}
