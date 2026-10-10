package dtos

import "time"

type Payload struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Profile   int8       `json:"profile"`
	LastLogin *time.Time `json:"lastlogin"`
}
