package error_type

import (
	"net/http"
)

type HTTPError struct {
	Code    int    // передается на клиент
	Message string // передаваемое на клиент общее сообщение об внутренней ошибке, либо заданное для клиентских
	Err     error  // оригинальная ошибка + доп информация для дальнейшего логирования во время записи ответа на клиент
}

// реализуем методы интерфейса типа error, чтобы возвращать его из функций типом error и иметь доступ к методам стандартного интерфейса
func (e HTTPError) Error() string {
	return e.Message
}

// указатель позволяет корректно пользоваться errors.As и проверять на nil
func NewBadRequest(msg string) *HTTPError {
	return &HTTPError{
		Code:    http.StatusBadRequest,
		Message: msg,
	}
}

// не уточняем для клиента, почему конкретно произошла ошибка, чтобы не допускать уязвимостей
// по типу пользователь с такой почтой еще не зарегистрирован и т п
func NewUnauthorized(msg string) *HTTPError {
	return &HTTPError{
		Code:    http.StatusUnauthorized,
		Message: msg,
	}
}

func NewConflict(msg string) *HTTPError {
	return &HTTPError{
		Code:    http.StatusConflict,
		Message: msg,
	}
}

func NewInternal(err error) *HTTPError {
	return &HTTPError{
		Code:    http.StatusInternalServerError,
		Message: "internal server error",
		Err:     err,
	}
}

// var ( // для квалицикации ошибок чтобы потом понимать в транспорте, какой статус код возвращать и какая конкретно ошибка произошла
// 	// статус код 409
// 	ErrUserAlreadyExists = errors.New("Пользователь с такой почтой уже существует")
// 	// статус код 401
// 	ErrWrongPassword = errors.New("Неправильный пароль")
// 	// статус код 400
// 	ErrWrongJSON        = errors.New("Не удалось распарсить JSON")
// 	ErrWrongRegistrData = errors.New("Почта или пароль не соответствуют требованиям")
// 	// статус код 500
// 	ErrDatabase             = errors.New("Ошибка при работе с базой данных")
// 	ErrGenerateHashPassword = errors.New("Ошибка при генерации хеша для заданного пароля")
// 	ErrGenerateTokens       = errors.New("Ошибка при генерации JWT токенов для входа")
// 	ErrGenerateHashToken    = errors.New("Ошибка при генерации хеша для заданного токена")
// )
