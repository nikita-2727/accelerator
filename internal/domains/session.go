package domains

import "time"

type Session struct {
	UserID    string
	TokenHash string
	TokenJTI  string
	RevokedAt *time.Time // чтобы осуществлять проверку на nil, используем указатель
	CreatedAt time.Time
	ExpiresAt time.Time
}
