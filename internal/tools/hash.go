package tools

import (
	"golang.org/x/crypto/bcrypt"
	"crypto/sha256"
	"encoding/hex"
)

// генерация хеша из строки, возвращает ошибку
func GeneratePasswordHash(str string) (string, error) {
	// оказывается зашивает не только хэш, но и случайную соль, чтобы хеш у одинаковых паролей был разный
	// и нельзя было подобрать хеши для всех популярных паролей и использовать их
	bytes, err := bcrypt.GenerateFromPassword([]byte(str), bcrypt.DefaultCost)
	return string(bytes), err
}

// проверка строки и хэша, если равны, то true
func ComparePasswordHash(hash, str string) bool {
	// сам вычленяет соль из пароля и проверяет ее с другой строкой и этой же солью
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(str))
	return err == nil
}


// отдельная функция для хеширования токенов без использования bcrypt, поскольку для нее они длиннее
// максимальная длина входных данных — 72 байта, так что используем обычный SHA
func GenerateTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func CompareTokenHash(hash, token string) bool {
	return GenerateTokenHash(token) == hash
}