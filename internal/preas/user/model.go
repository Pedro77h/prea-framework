package user

import "time"

type User struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Username  string     `json:"username"`
	Email     string     `json:"email"`
	Password  string     `json:"-"`
	Profile   int8       `json:"profile"`
	Token     *string    `json:"-"` // id (jti) of the active session token
	LastLogin *time.Time `json:"lastlogin" db:"lastlogin"`
}

func (User) Table() string {
	return "users"
}
