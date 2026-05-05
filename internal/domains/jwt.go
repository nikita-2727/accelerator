package domains

import "time"

type ReturnCreateTokensInfo struct {
	AccessToken       string    // возвращаем на клиент
	RefreshToken      string    // возвращаем на клиент
	RefreshJTI        string    // уникальный идентификатор refresh-токена
	CreateTime        time.Time // для дальнейшей записи в бд сессии
	AccessExpireTime  time.Time // возвращаем на клиент
	RefreshExpireTime time.Time // для дальнейшей записи в бд сессии
}
