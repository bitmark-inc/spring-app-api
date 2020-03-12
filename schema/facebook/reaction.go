package facebook

import (
	"time"

	"github.com/alecthomas/jsonschema"
	"github.com/google/uuid"
	"github.com/xeipuuv/gojsonschema"
)

type RawReactions struct {
	Reactions []*Reaction `json:"reactions" jsonschema:"required"`
}

type Reaction struct {
	Timestamp   int64             `json:"timestamp" jsonschema:"required"`
	Title       MojibakeString    `json:"title" jsonschema:"required"`
	Data        []ReactionWrapper `json:"data"`
	Attachments []*Attachment     `json:"attachments"`
}

type ReactionWrapper struct {
	Reaction ReactionData `json:"reaction" jsonschema:"required"`
}

type ReactionData struct {
	Reaction string         `json:"reaction" jsonschema:"required"`
	Actor    MojibakeString `json:"actor" jsonschema:"required"`
}

func ReactionSchemaLoader() *gojsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  true,
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: true,
	}
	s := reflector.Reflect(&RawReactions{})
	data, _ := s.MarshalJSON()
	schemaLoader := gojsonschema.NewStringLoader(string(data))
	schema, _ := gojsonschema.NewSchema(schemaLoader)
	return schema
}

type ReactionORM struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	Timestamp   int64     `gorm:"unique_index:facebook_reaction_owner_timestamp_unique"`
	Date        string
	Weekday     int
	Title       string
	Actor       string
	Reaction    string
	DataOwnerID string `gorm:"unique_index:facebook_reaction_owner_timestamp_unique"`
}

func (ReactionORM) TableName() string {
	return "facebook_reaction"
}

func (r RawReactions) ORM(owner string) []interface{} {
	idx := 0
	result := make([]interface{}, 0)
	for _, r := range r.Reactions {
		t := time.Unix(int64(r.Timestamp), 0)
		orm := ReactionORM{
			Timestamp:   r.Timestamp,
			Date:        dateOfTime(t),
			Weekday:     weekdayOfTime(t),
			Title:       string(r.Title),
			DataOwnerID: owner,
		}
		if len(r.Data) > 0 {
			orm.Actor = string(r.Data[0].Reaction.Actor)
			orm.Reaction = string(r.Data[0].Reaction.Reaction)
		}

		result = append(result, orm)
		idx++
	}
	return result
}
