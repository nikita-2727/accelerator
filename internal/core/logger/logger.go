package logger

import (
	"fmt"
	"log/slog"
	"os"
)

// инициализируем файл для записи логов и настраиваем логгер
func InitLog() {
	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println("Ошибка при создании  файла для логов")
	}
	handler := slog.NewJSONHandler(file, nil)
	logger := slog.New(handler)
	slog.SetDefault(logger)
}
