package domain

import "time"

// User is the persisted VPN account model.
type User struct {
	ID          string            `json:"id" bson:"_id"`
	Name        string            `json:"name" bson:"name"`
	CoreID      string            `json:"core_id,omitempty" bson:"core_id"`
	AdapterType string            `json:"adapter_type,omitempty" bson:"adapter_type"`
	Credentials map[string]string `json:"credentials,omitempty" bson:"creds"`

	Upload   int64 `json:"upload" bson:"upload"`
	Download int64 `json:"download" bson:"download"`

	Quota          int64      `json:"quota" bson:"quota"`
	SpeedLimitUp   int64      `json:"speed_limit_up" bson:"slup"`
	SpeedLimitDown int64      `json:"speed_limit_down" bson:"sldown"`
	ExpireAt       *time.Time `json:"expire_at,omitempty" bson:"expire_at"`

	Enabled   bool      `json:"enabled" bson:"enabled"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`

	SpeedUp   int64 `json:"speed_up" bson:"-"`
	SpeedDown int64 `json:"speed_down" bson:"-"`
}
