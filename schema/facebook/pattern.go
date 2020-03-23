package facebook

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/afero"
	"github.com/xeipuuv/gojsonschema"
)

var (
	FriendsPattern        = Pattern{Name: "friends", Location: "friends", Regexp: regexp.MustCompile("^friends.json"), Schema: FriendSchemaLoader()}
	PostsPattern          = Pattern{Name: "posts", Location: "posts", Regexp: regexp.MustCompile("your_posts(?P<index>_[0-9]+).json"), Schema: PostArraySchemaLoader()}
	ReactionsPattern      = Pattern{Name: "reactions", Location: "likes_and_reactions", Regexp: regexp.MustCompile("posts_and_comments.json"), Schema: ReactionSchemaLoader()}
	CommentsPattern       = Pattern{Name: "comments", Location: "comments", Regexp: regexp.MustCompile("comments.json"), Schema: CommentArraySchemaLoader()}
	InvitedEventPattern   = Pattern{Name: "invited_events", Location: "events", Regexp: regexp.MustCompile("event_invitations.json"), Schema: InvitedEventSchemaLoader()}
	RespondedEventPattern = Pattern{Name: "responded_events", Location: "events", Regexp: regexp.MustCompile("your_event_responses.json"), Schema: RespondedEventSchemaLoader()}
	MediaPattern          = Pattern{Name: "media", Location: "photos_and_videos"}
	FilesPattern          = Pattern{Name: "files", Location: "files"}
)

type Pattern struct {
	Name     string
	Location string
	Regexp   *regexp.Regexp
	Schema   *gojsonschema.Schema
}

func (p *Pattern) SelectFiles(fs afero.Fs, dirname string) ([]string, error) {
	targetedFiles := make([]string, 0)

	exists, err := afero.Exists(fs, dirname)
	if err != nil {
		return nil, fmt.Errorf("failed to check if dir %s exists: %s", dirname, err)
	}
	if !exists {
		return nil, nil
	}

	files, err := afero.ReadDir(fs, dirname)
	if err != nil {
		return nil, fmt.Errorf("failed to read dir %s: %s", dirname, err)
	}
	for _, f := range files {
		if p.Regexp.MatchString(f.Name()) {
			targetedFiles = append(targetedFiles, filepath.Join(dirname, f.Name()))
		}
	}

	return targetedFiles, nil
}

func (p *Pattern) Validate(data []byte) error {
	docLoader := gojsonschema.NewBytesLoader(data)
	result, err := p.Schema.Validate(docLoader)
	if err != nil {
		return err
	}
	if !result.Valid() {
		reasons := make([]string, 0)
		for _, desc := range result.Errors() {
			reasons = append(reasons, desc.String())
		}
		return errors.New(strings.Join(reasons, "\n"))
	}
	return nil
}
