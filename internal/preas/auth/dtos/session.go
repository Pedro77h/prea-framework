package dtos

type Session struct {
	Token   string  `json:"token"`
	Payload Payload `json:"payload"`
}
