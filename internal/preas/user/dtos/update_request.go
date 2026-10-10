package dtos

type UpdateRequest struct {
	Name     string `json:"name" form:"name"`
	Username string `json:"username" form:"username"`
}
