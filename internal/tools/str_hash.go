package tools

import (
	"golang.org/x/crypto/bcrypt"
)

// генерация хеша из строки, возвращает ошибку
func GenerateHash(str string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(str), bcrypt.DefaultCost)
	return string(bytes), err
}

// проверка строки и хэша, если равны, то true
func CompareHash(str, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(str))
	return err == nil
}
