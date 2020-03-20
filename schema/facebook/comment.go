package facebook

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/alecthomas/jsonschema"
	"github.com/google/uuid"
	"github.com/xeipuuv/gojsonschema"
)

type RawComments struct {
	Comments []Comment `json:"comments" jsonschema:"required"`
}

type Comment struct {
	Timestamp   int64             `json:"timestamp" jsonschema:"required"`
	Title       MojibakeString    `json:"title" jsonschema:"required"`
	Data        []*CommentWrapper `json:"data"`
	Attachments []*Attachment     `json:"attachments"`
}

type CommentWrapper struct {
	Comment CommentData `json:"comment" jsonschema:"required"`
}

type CommentData struct {
	Timestamp int            `json:"timestamp" jsonschema:"required"`
	Comment   MojibakeString `json:"comment" jsonschema:"required"`
	Author    MojibakeString `json:"author"`
	Group     MojibakeString `json:"group"`
}

func CommentArraySchemaLoader() *gojsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  true,
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: true,
	}
	s := reflector.Reflect(&RawComments{})
	data, _ := s.MarshalJSON()
	schemaLoader := gojsonschema.NewStringLoader(string(data))
	schema, _ := gojsonschema.NewSchema(schemaLoader)
	return schema
}

type CommentORM struct {
	ID                    uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	Timestamp             int64     `gorm:"unique_index:facebook_comment_owner_timestamp_unique"`
	Author                string
	Comment               string
	Date                  string
	Weekday               int
	DataOwnerID           string `gorm:"unique_index:facebook_comment_owner_timestamp_unique"`
	MediaAttached         bool
	ExternalContextURL    string
	ExternalContextSource string
	ExternalContextName   string
	MediaItems            []CommentMediaORM `gorm:"foreignkey:CommentID;association_foreignkey:ID" json:"-"`
	ConflictFlag          bool
}

func (CommentORM) TableName() string {
	return "facebook_comment"
}

// FIXME: This ORM is almost the same to PostMediaORM. It is expected to merger together.
type CommentMediaORM struct {
	ID                uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	MediaURI          string
	ThumbnailURI      string
	FilenameExtension string
	Timestamp         int64      `gorm:"unique_index:facebook_commentmedia_owner_comment_id_timestamp_unique"`
	MediaIndex        int64      `gorm:"unique_index:facebook_commentmedia_owner_comment_id_timestamp_unique"`
	DataOwnerID       string     `gorm:"unique_index:facebook_commentmedia_owner_comment_id_timestamp_unique"`
	Comment           CommentORM `gorm:"foreignkey:CommentID" json:"-"`
	CommentID         uuid.UUID  `gorm:"unique_index:facebook_commentmedia_owner_comment_id_timestamp_unique"`
}

func (CommentMediaORM) TableName() string {
	return "facebook_commentmedia"
}

func (c RawComments) ORM(dataOwner string, archiveID string) ([]interface{}, []CommentORM) {
	idx := 0
	result := make([]interface{}, 0)
	complicatedComments := []CommentORM{}
	for _, c := range c.Comments {
		t := time.Unix(int64(c.Timestamp), 0)
		comment := CommentORM{
			Timestamp:   c.Timestamp,
			Date:        dateOfTime(t),
			Weekday:     weekdayOfTime(t),
			DataOwnerID: dataOwner,
		}
		if len(c.Data) > 0 {
			comment.Author = string(c.Data[0].Comment.Author)
			comment.Comment = string(c.Data[0].Comment.Comment)
		}

		if len(c.Attachments) == 0 {
			result = append(result, comment)
		} else {
			for _, a := range c.Attachments {
				for i, item := range a.Data {
					if item.Media != nil {
						comment.MediaAttached = true
						uri := fmt.Sprintf("%s/facebook/archives/%s/data/%s", dataOwner, archiveID, string(item.Media.URI))

						mediaTimestamp := int64(item.Media.CreationTimestamp)
						if mediaTimestamp == 0 {
							mediaTimestamp = comment.Timestamp
						}

						commentMedia := CommentMediaORM{
							MediaURI:          uri,
							ThumbnailURI:      uri,
							Timestamp:         mediaTimestamp,
							MediaIndex:        int64(i),
							FilenameExtension: filepath.Ext(string(item.Media.URI)),
							DataOwnerID:       dataOwner,
						}
						if item.Media.Thumbnail != nil {
							commentMedia.ThumbnailURI = fmt.Sprintf("%s/facebook/archives/%s/data/%s", dataOwner, archiveID, string(item.Media.Thumbnail.URI))
						}
						comment.MediaItems = append(comment.MediaItems, commentMedia)
					}
					if item.ExternalContext != nil {
						comment.ExternalContextName = string(item.ExternalContext.Name)
						comment.ExternalContextSource = string(item.ExternalContext.Source)
						comment.ExternalContextURL = string(item.ExternalContext.URL)
					}
				}
			}
			complicatedComments = append(complicatedComments, comment)
		}

		idx++
	}
	return result, complicatedComments
}
