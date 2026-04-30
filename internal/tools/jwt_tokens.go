package tools

import (
	"accelerator/internal/core/config"
	"accelerator/internal/domains"

	"time"

	"github.com/golang-jwt/jwt/v5"
)

// возвращаем два токена и ошибку
// сначала access, потом refresh
func GenerateJWTToken(userID string, cfg *config.Config) (*domains.ReturnCreateTokensInfo, error) {
	createTime := time.Now()
	accessExpareTime := createTime.Add(cfg.AccessTime)
	refreshExpareTime := createTime.Add(cfg.RefreshTime)

	accessClaims := jwt.MapClaims{
		"user_id": userID,
		"exp":     accessExpareTime.Unix(), // время истечения срока токена
		"iat":     createTime.Unix(),       // время создания токена
	}

	refreshClaims := jwt.MapClaims{
		"user_id": userID,
		"exp":     refreshExpareTime.Unix(),
		"iat":     createTime.Unix(),
	}

	// создание не подписанного токена, просто хранится структура с полезной нагрузкой, HMAC-SHA256 (симметричный алгоритм) - один и тот же ключ для шифрования и проверки
	accessTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	refreshTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)

	/*
		Берёт заголовок (header) и преобразует его в JSON → кодирует в Base64URL.
		Берёт полезную нагрузку (claims) – преобразует в JSON (благодаря тегам json у структуры) → кодирует в Base64URL.
		Склеивает две части через точку: base64Header.base64Payload.

		Вычисляет подпись:
		Берёт получившуюся строку base64Header.base64Payload
		Применяет HMAC-SHA256 с использованием cfg.AccessSecret
		Результат (HMAC digest) кодирует в Base64URL.
		Добавляет подпись через точку: base64Header.base64Payload.base64Signature.
	*/
	accessToken, err := accessTokenObj.SignedString(cfg.JWTAccessSecret)
	if err != nil {
		return nil, err
	}

	refreshToken, err := refreshTokenObj.SignedString(cfg.JWTRefreshSecret)
	if err != nil {
		return nil, err
	}

	// создаем структуру для передачи в сервис
	infoTokens := &domains.ReturnCreateTokensInfo{
		AccessToken:       accessToken,
		RefreshToken:      refreshToken,
		CreateTime:        createTime,
		AccessExpireTime:  accessExpareTime,
		RefreshExpireTime: refreshExpareTime,
	}

	return infoTokens, nil

}
