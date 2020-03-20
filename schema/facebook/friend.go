package facebook

import (
	"github.com/alecthomas/jsonschema"
	"github.com/google/uuid"
	"github.com/xeipuuv/gojsonschema"
)

type RawFriends struct {
	Friends []*Friend `json:"friends" jsonschema:"required"`
}

type Friend struct {
	Timestamp   int64          `json:"timestamp" jsonschema:"required"`
	Name        MojibakeString `json:"name" jsonschema:"required"`
	ContactInfo MojibakeString `json:"contact_info"`
}

func FriendSchemaLoader() *gojsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  true,
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: true,
	}
	s := reflector.Reflect(&RawFriends{})
	data, _ := s.MarshalJSON()
	schemaLoader := gojsonschema.NewStringLoader(string(data))
	schema, _ := gojsonschema.NewSchema(schemaLoader)
	return schema
}

type FriendORM struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	FriendName  string
	Timestamp   int64  `gorm:"unique_index:facebook_friend_owner_timestamp_unique"`
	DataOwnerID string `gorm:"unique_index:facebook_friend_owner_timestamp_unique"`
}

func (FriendORM) TableName() string {
	return "facebook_friend"
}

// FIXME: friends can have the same name
func (r RawFriends) ORM(owner string) []interface{} {
	idx := 0
	result := make([]interface{}, 0)

	seen := make(map[string]bool)
	for _, f := range r.Friends {
		name := string(f.Name)
		if seen[name] {
			continue
		}
		seen[name] = true

		orm := FriendORM{
			FriendName:  name,
			Timestamp:   f.Timestamp,
			DataOwnerID: owner,
		}

		result = append(result, orm)
		idx++
	}
	return result
}
