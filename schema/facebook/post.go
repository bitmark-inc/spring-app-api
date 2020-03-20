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
	Timestamp             int64 `gorm:"unique_index:facebook_post_owner_timestamp_unique"`
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
	DataOwnerID           string         `gorm:"unique_index:facebook_post_owner_timestamp_unique"`
	MediaItems            []PostMediaORM `gorm:"foreignkey:PostID;association_foreignkey:ID" json:"-"`
	Places                []PlaceORM     `gorm:"foreignkey:PostID;association_foreignkey:ID" json:"-"`
	Tags                  []TagORM       `gorm:"foreignkey:PostID;association_foreignkey:ID" json:"-"`
	ConflictFlag          bool
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
	ThumbnailURI      string
	FilenameExtension string
	Timestamp         int64     `gorm:"unique_index:facebook_postmedia_owner_post_id_timestamp_unique"`
	MediaIndex        int64     `gorm:"unique_index:facebook_postmedia_owner_post_id_timestamp_unique"`
	DataOwnerID       string    `gorm:"unique_index:facebook_postmedia_owner_post_id_timestamp_unique"`
	Post              PostORM   `gorm:"foreignkey:PostID" json:"-"`
	PostID            uuid.UUID `gorm:"unique_index:facebook_postmedia_owner_post_id_timestamp_unique"`
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
	DataOwnerID string    `gorm:"unique_index:facebook_place_owner_timestamp_unique"`
	Post        PostORM   `gorm:"foreignkey:PostID" json:"-"`
	PostID      uuid.UUID `gorm:"unique_index:facebook_place_owner_timestamp_unique"` // NOTE:  one place per post
}

func (PlaceORM) TableName() string {
	return "facebook_place"
}

type TagORM struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	DataOwnerID string    `gorm:"unique_index:facebook_tag_owner_post_friend_unique"`
	Post        PostORM   `gorm:"foreignkey:PostID" json:"-"`
	PostID      uuid.UUID `gorm:"unique_index:facebook_tag_owner_post_friend_unique"`
	Friend      FriendORM `gorm:"foreignkey:FriendID" json:"-"`
	FriendID    uuid.UUID `gorm:"unique_index:facebook_tag_owner_post_friend_unique"`
	FriendName  string
}

func (TagORM) TableName() string {
	return "facebook_tag"
}

type RawPosts struct {
	Items []*RawPost
}

func (r *RawPosts) ORM(dataOwner, archiveID string, beginTime, endTime int64) ([]interface{}, []PostORM) {
	posts := make([]interface{}, 0)
	complexPosts := make([]PostORM, 0)

	for _, rp := range r.Items {
		t := int64(rp.Timestamp)
		if t >= beginTime && t <= endTime { // omit post within current activity range
			continue
		}
		ts := time.Unix(t, 0)
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
			for i, item := range a.Data {
				if item.Media != nil {
					post.MediaAttached = true
					uri := fmt.Sprintf("%s/facebook/archives/%s/data/%s", dataOwner, archiveID, string(item.Media.URI))

					mediaTimestamp := int64(item.Media.CreationTimestamp)
					if mediaTimestamp == 0 {
						mediaTimestamp = post.Timestamp
					}

					postMedia := PostMediaORM{
						MediaURI:          uri,
						ThumbnailURI:      uri,
						Timestamp:         mediaTimestamp,
						MediaIndex:        int64(i),
						FilenameExtension: filepath.Ext(string(item.Media.URI)),
						DataOwnerID:       dataOwner,
					}
					if item.Media.Thumbnail != nil {
						postMedia.ThumbnailURI = fmt.Sprintf("%s/facebook/archives/%s/data/%s", dataOwner, archiveID, string(item.Media.Thumbnail.URI))
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
		AllowAdditionalProperties:  true,
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
