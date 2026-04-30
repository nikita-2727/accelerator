package tools

import (
	"accelerator/internal/core/error_type"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// принимает любой тип структуры и записывает в ответ вместе со статусом
// обрабатывает и логирует ошибки записи
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("Не удалось записать ответ на клиент: ", "error", err)
	}
}

// вычленяет кастомный тип ошибки error_type.HHTPError из интерфейса error и записывает ответ об ошибке на клиент
// подробно логирует пришедшие ошибки, в также ошибки записи
func WriteError(w http.ResponseWriter, err error) {
	// если эта кастомная ошибка, то логируем со всеми подробностями, ставим из нее статус код и message
	// новая запись в go 1.26 вместо объявления переменной и errors.As()
	if httpErr, ok := errors.AsType[*error_type.HTTPError](err); ok {
		slog.Error("sending HTTP error",
			"status", httpErr.Code,
			"client_message", httpErr.Message,
			"internal_error", httpErr.Err, // оригинальная ошибка (может быть nil для клиентских ошибок) + доп инфа с fmt.Errorf
		)

		// отправляем ответ клиенту
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpErr.Code)

		if err := json.NewEncoder(w).Encode(map[string]string{
			"error": httpErr.Message,
		}); err != nil {
			slog.Error("Не удалось записать ошибку на клиент: ", "error", err)
		}

	} else {
		// если это незадокументированная ошибка, которую я не обрабатывал кастамным типом
		slog.Error("undocumented server error",
			"status", http.StatusInternalServerError,
			"internal_error", err.Error(),
		)

		// отправляем ответ клиенту
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"error": "непредвиденная ошибка сервера",
		}); err != nil {
			slog.Error("Не удалось записать ошибку на клиент: ", "error", err)
		}
	}

}
