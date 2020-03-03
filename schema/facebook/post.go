package facebook

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/alecthomas/jsonschema"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/xeipuuv/gojsonschema"
)

type PostORM struct {
	ID                    uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	PostID                int64
	Timestamp             int64
	UpdateTimestamp       int64
	Date                  string
	Weekday               int
	Title                 string
	Post                  string
	ExternalContextURL    string
	ExternalContextSource string
	ExternalContextName   string
	EventName             string
	EventStartTimestamp   int64
	EventEndTimestamp     int64
	MediaAttached         bool
	Sentiment             string
	DataOwnerID           string
	MediaItems            []PostMediaORM `gorm:"foreignkey:PostID;association_foreignkey:ID"`
	Places                []PlaceORM     `gorm:"foreignkey:PostID;association_foreignkey:ID"`
	Tags                  []TagORM       `gorm:"foreignkey:PostID;association_foreignkey:ID"`
}

func (PostORM) TableName() string {
	return "facebook_post"
}

func (p *PostORM) BeforeCreate(scope *gorm.Scope) error {
	uuid, err := uuid.NewUUID()
	if err != nil {
		return err
	}
	return scope.SetColumn("ID", uuid)
}

type PostMediaORM struct {
	ID                uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	MediaURI          string
	FilenameExtension string
	DataOwnerID       string
	Post              PostORM `gorm:"foreignkey:PostID"`
	PostID            uuid.UUID
}

func (PostMediaORM) TableName() string {
	return "facebook_postmedia"
}

type PlaceORM struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	Name        string
	Address     string
	Latitude    float64
	Longitude   float64
	DataOwnerID string
	Post        PostORM `gorm:"foreignkey:PostID"`
	PostID      uuid.UUID
}

func (PlaceORM) TableName() string {
	return "facebook_place"
}

type TagORM struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	DataOwnerID string
	Post        PostORM `gorm:"foreignkey:PostID"`
	PostID      uuid.UUID
	Friend      FriendORM `gorm:"foreignkey:FriendID"`
	FriendID    uuid.UUID
	FriendName  string
}

func (TagORM) TableName() string {
	return "facebook_tag"
}

type RawPosts struct {
	Items []*RawPost
}

func (r *RawPosts) ORM(dataOwner, archiveID string) ([]interface{}, []PostORM) {
	posts := make([]interface{}, 0)
	complexPosts := make([]PostORM, 0)

	for _, rp := range r.Items {
		ts := time.Unix(int64(rp.Timestamp), 0)
		post := PostORM{
			Timestamp:   rp.Timestamp,
			Date:        dateOfTime(ts),
			Weekday:     weekdayOfTime(ts),
			Title:       string(rp.Title),
			DataOwnerID: dataOwner,
		}

		for _, d := range rp.Data {
			if d.Post != "" {
				post.Post = string(d.Post)
				post.Sentiment = sentiment(string(d.Post))
			}
			if d.UpdateTimestamp != 0 {
				post.UpdateTimestamp = d.UpdateTimestamp
			}
		}

		complex := false
		for _, a := range rp.Attachments {
			for _, item := range a.Data {
				if item.Media != nil {
					post.MediaAttached = true
					uri := fmt.Sprintf("%s/facebook/archives/%s/data/%s", dataOwner, archiveID, string(item.Media.URI))
					postMedia := PostMediaORM{
						MediaURI:          uri,
						FilenameExtension: filepath.Ext(string(item.Media.URI)),
						DataOwnerID:       dataOwner,
					}
					post.MediaItems = append(post.MediaItems, postMedia)
					complex = true
				}
				if item.ExternalContext != nil {
					post.ExternalContextName = string(item.ExternalContext.Name)
					post.ExternalContextSource = string(item.ExternalContext.Source)
					post.ExternalContextURL = string(item.ExternalContext.URL)
				}
				if item.Event != nil {
					post.EventName = string(item.Event.Name)
					post.EventStartTimestamp = item.Event.StartTimestamp
					post.EventEndTimestamp = item.Event.EndTimestamp
				}
				if item.Place != nil {
					place := PlaceORM{
						Name:        string(item.Place.Name),
						Address:     string(item.Place.Address),
						DataOwnerID: dataOwner,
					}
					if item.Place.Coordinate != nil {
						place.Latitude = item.Place.Coordinate.Latitude
						place.Longitude = item.Place.Coordinate.Longitude
					}
					post.Places = append(post.Places, place)
					complex = true
				}
			}
		}

		if len(rp.Tags) > 0 {
			for _, t := range rp.Tags {
				tag := TagORM{
					DataOwnerID: dataOwner,
					FriendName:  string(t),
				}
				post.Tags = append(post.Tags, tag)
				complex = true
			}
		}

		if complex {
			complexPosts = append(complexPosts, post)
		} else {
			posts = append(posts, post)
		}
	}

	return posts, complexPosts
}

type RawPost struct {
	Timestamp   int64            `json:"timestamp" jsonschema:"required"`
	Title       MojibakeString   `json:"title"`
	Data        []*PostData      `json:"data" jsonschema:"maxItems=2"`
	Attachments []*Attachment    `json:"attachments"`
	Tags        []MojibakeString `json:"tags"`
}

type PostData struct {
	Post               MojibakeString `json:"post"`
	UpdateTimestamp    int64          `json:"update_timestamp"`
	BackdatedTimestamp int64          `json:"backdated_timestamp"`
}

func PostArraySchemaLoader() *gojsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  false,
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: true,
	}
	postSchema := reflector.Reflect(&RawPost{})
	postsSchema := &jsonschema.Schema{Type: &jsonschema.Type{
		Version: jsonschema.Version,
		Type:    "array",
		Items:   postSchema.Type,
	}, Definitions: postSchema.Definitions}

	data, _ := postsSchema.MarshalJSON()
	schemaLoader := gojsonschema.NewStringLoader(string(data))
	schema, _ := gojsonschema.NewSchema(schemaLoader)
	return schema
}
