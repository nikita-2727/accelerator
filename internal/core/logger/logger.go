package logger

import (
	"fmt"
	"log/slog"
	"os"
)

// инициализируем файл для записи логов и настраиваем логгер
func InitLog() {
	err := os.MkdirAll("logs", 0755)
	if err != nil { // если уже создана, не возвращает ошибку
		fmt.Println("Ошибка при создании директории для логов")
		return
	}
	file, err := os.OpenFile("logs/app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println("Ошибка при создании файла для логов")
		return
	}
	handler := slog.NewJSONHandler(file, nil)
	logger := slog.New(handler)
	slog.SetDefault(logger)
}
