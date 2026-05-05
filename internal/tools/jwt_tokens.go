package tools

import (
	"accelerator/internal/core/config"
	"accelerator/internal/domains"
	"fmt"

	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// возвращаем два токена и ошибку
// сначала access, потом refresh
func GenerateJWTToken(userID string, cfg *config.Config) (*domains.ReturnCreateTokensInfo, error) {
	createTime := time.Now()
	accessExpareTime := createTime.Add(cfg.AccessTime)
	refreshExpareTime := createTime.Add(cfg.RefreshTime)
	// генерируем уникальный идентификатор рефреш токена для быстрого поиска сессии в базе данных без поиска по хешу
	refreshJTI := generateJTI()

	accessClaims := jwt.MapClaims{
		"user_id": userID,
		"exp":     accessExpareTime.Unix(), // время истечения срока токена
		"iat":     createTime.Unix(),       // время создания токена
	}

	refreshClaims := jwt.MapClaims{
		"user_id": userID,
		"jti":     refreshJTI,
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
	accessToken, err := accessTokenObj.SignedString([]byte(cfg.JWTAccessSecret)) // принимает секретный ключ как массив байтов
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	refreshToken, err := refreshTokenObj.SignedString([]byte(cfg.JWTRefreshSecret))
	if err != nil {
		return nil, fmt.Errorf("sign refresh token: %w", err)
	}

	// создаем структуру для передачи в сервис
	infoTokens := &domains.ReturnCreateTokensInfo{
		AccessToken:       accessToken,
		RefreshToken:      refreshToken,
		RefreshJTI:        refreshJTI,
		CreateTime:        createTime,
		AccessExpireTime:  accessExpareTime,
		RefreshExpireTime: refreshExpareTime,
	}

	return infoTokens, nil

}


// парсит токен, проверяет подпись и срок действия
// Возвращает мапу с параметрами токена и ошибку
func ParseAndValidateToken(tokenString string, secret []byte) (jwt.MapClaims, error) {
	// для проверки токена не нужно парсить и получать created_at и expires_at, но они могут понадобятся для дальнейшего удал
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Проверяем, что используется ожидаемый алгоритм подписи
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	// получаем загруженные параметры из токена
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("cannot cast claims to MapClaims: %w", err)
	}

	return claims, nil
}

// генерирует случайный идентификатор для токена
func generateJTI() string {
	return uuid.New().String()
}




// возвращает userID из access-токена и валидирует его
func ParseAccessToken(tokenString string, cfg *config.Config) (string, error) {
    claims, err := ParseAndValidateToken(tokenString, []byte(cfg.JWTAccessSecret))
    if err != nil {
        return "", err
    }
	// проверяем, есть ли в параметрах токена id
    userID, ok := claims["user_id"].(string)
    if !ok {
        return "", fmt.Errorf("user_id claim missing or not a string: %w", err)
    }

    return userID, nil
}

// возвращает userID и jti из refresh-токена и валидирует его
// jti используется для поиска сессии в БД
func ParseRefreshToken(tokenString string, cfg *config.Config) (string, string, error) {
    claims, err := ParseAndValidateToken(tokenString, []byte(cfg.JWTRefreshSecret))
    if err != nil {
        return "", "", err
    }
	// проверяем, есть ли в параметрах токена id
    userID, ok := claims["user_id"].(string)
    if !ok {
        return "", "", fmt.Errorf("user_id claim missing or not a string: %w", err)
    }
	// проверяем, есть ли в параметрах токена jti
    jtiClaim, ok := claims["jti"]
    if !ok {
        return "", "", fmt.Errorf("jti claim missing in refresh token: %w", err)
    }
	// можно ли привести jti к строке, или он получится невалидный
    jti, ok := jtiClaim.(string)
    if !ok {
        return "", "", fmt.Errorf("jti claim is not a string: %w", err)
    }

    return userID, jti, nil
}
