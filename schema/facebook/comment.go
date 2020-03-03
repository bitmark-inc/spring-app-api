package facebook

import (
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
		AllowAdditionalProperties:  false,
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
	ID          uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	Timestamp   int64
	Author      string
	Comment     string
	Date        string
	Weekday     int
	DataOwnerID string
}

func (CommentORM) TableName() string {
	return "facebook_comment"
}

func (c RawComments) ORM(owner string) []interface{} {
	idx := 0
	result := make([]interface{}, 0)
	for _, c := range c.Comments {
		t := time.Unix(int64(c.Timestamp), 0)
		orm := CommentORM{
			Timestamp:   c.Timestamp,
			Date:        dateOfTime(t),
			Weekday:     weekdayOfTime(t),
			DataOwnerID: owner,
		}
		if len(c.Data) > 0 {
			orm.Author = string(c.Data[0].Comment.Author)
			orm.Comment = string(c.Data[0].Comment.Comment)
		}

		result = append(result, orm)
		idx++
	}
	return result
}
