package domains

import "time"

type User struct {
	ID            string
	Email         string
	Password_hash string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
