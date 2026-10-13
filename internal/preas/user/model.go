package user

import "time"

type User struct {
	ID        string     `json:"id" pk:"" generated:"uuid"`
	Name      string     `json:"name"`
	Username  string     `json:"username" unique:""`
	Email     string     `json:"email" unique:""`
	Password  string     `json:"-"`
	Profile   int8       `json:"profile"`
	Token     *string    `json:"-"` // id (jti) of the active session token
	LastLogin *time.Time `json:"lastlogin" db:"lastlogin"`
}

func (User) Table() string {
	return "users"
}
