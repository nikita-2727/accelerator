package tools

import (
	"github.com/go-playground/validator/v10"
)

func GetMessageFromValidateError(err error) string {
	// Приводим ошибку к типу ValidationErrors
	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		// Если это не ValidationErrors - обрабатываем как общую ошибку
		return "Ошибка валидации"
	}

	// пробегаем по всем ошибкам, берем первую попавшуюся
	for _, fieldError := range validationErrors {
		// Получаем имя поля, используя JSON-тег (например, "email" или "password")[reference:4]
		fieldName := fieldError.Field()

		// Формируем понятное сообщение об ошибке в зависимости от нарушенного правила
		switch fieldError.Tag() {
		case "required":
			return  "Поле обязательно для заполнения: " + fieldName
		case "email":
			return  "Введите корректный email адрес"
		case "min":
			return "Минимальная длина поля - 8 символов: " + fieldName
		default:
			return "Некорректное значение поля"
		}
	}

	return "Ошибка валидации"
}
