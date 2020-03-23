package facebook

import (
	"github.com/alecthomas/jsonschema"
	"github.com/google/uuid"
	"github.com/xeipuuv/gojsonschema"
)

type EventORM struct {
	ID             uuid.UUID `json:"id" gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	StartTimestamp int64     `json:"start_timestamp" gorm:"unique_index:facebook_event_owner_start_end_timestamp_unique"`
	EndTimestamp   int64     `json:"end_timestamp" gorm:"unique_index:facebook_event_owner_start_end_timestamp_unique"`
	DataOwnerID    string    `json:"-" gorm:"unique_index:facebook_event_owner_start_end_timestamp_unique"`
}

func (EventORM) TableName() string {
	return "facebook_event"
}

type RawInvitedEvent struct {
	EventsInvited []*Event `json:"events_invited" jsonschema:"required"`
}

func (r RawInvitedEvent) ORM(owner string) []interface{} {
	results := []interface{}{}
	for _, event := range r.EventsInvited {
		orm := &EventORM{
			Name:           string(event.Name),
			Type:           "INVITED",
			StartTimestamp: event.StartTimestamp,
			EndTimestamp:   event.EndTimestamp,
			DataOwnerID:    owner,
		}
		results = append(results, orm)
	}
	return results
}

func InvitedEventSchemaLoader() *gojsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  true,
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: true,
	}
	s := reflector.Reflect(&RawInvitedEvent{})
	data, _ := s.MarshalJSON()
	schemaLoader := gojsonschema.NewStringLoader(string(data))
	schema, _ := gojsonschema.NewSchema(schemaLoader)
	return schema
}

type RawRespondedEvent struct {
	EventResponses EventResponse `json:"event_responses" jsonschema:"required"`
}

type EventResponse struct {
	EventsJoined     []*Event `json:"events_joined"`
	EventsInterested []*Event `json:"events_interested"`
	EventsDeclined   []*Event `json:"events_declined"`
}

func (r RawRespondedEvent) ORM(owner string) []interface{} {
	results := []interface{}{}
	responses := r.EventResponses
	if len(responses.EventsJoined) > 0 {
		for _, event := range responses.EventsJoined {
			orm := &EventORM{
				Name:           string(event.Name),
				Type:           "JOINED",
				StartTimestamp: event.StartTimestamp,
				EndTimestamp:   event.EndTimestamp,
				DataOwnerID:    owner,
			}
			results = append(results, orm)
		}
	}
	if len(responses.EventsInterested) > 0 {
		for _, event := range responses.EventsInterested {
			orm := &EventORM{
				Name:           string(event.Name),
				Type:           "INTERESTED",
				StartTimestamp: event.StartTimestamp,
				EndTimestamp:   event.EndTimestamp,
				DataOwnerID:    owner,
			}
			results = append(results, orm)
		}
	}
	if len(responses.EventsDeclined) > 0 {
		for _, event := range responses.EventsDeclined {
			orm := &EventORM{
				Name:           string(event.Name),
				Type:           "DECLINED",
				StartTimestamp: event.StartTimestamp,
				EndTimestamp:   event.EndTimestamp,
				DataOwnerID:    owner,
			}
			results = append(results, orm)
		}
	}

	return results
}

func RespondedEventSchemaLoader() *gojsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  true,
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: true,
	}
	s := reflector.Reflect(&RawRespondedEvent{})
	data, _ := s.MarshalJSON()
	schemaLoader := gojsonschema.NewStringLoader(string(data))
	schema, _ := gojsonschema.NewSchema(schemaLoader)
	return schema
}
