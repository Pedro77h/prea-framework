package dtos

type ValidateRequest struct {
	Token string `json:"token" form:"token" binding:"required"`
}
