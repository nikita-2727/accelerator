package transport

import (
	"accelerator/internal/core/error_type"
	"accelerator/internal/features/auth/service"
	"accelerator/internal/tools"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
)

type AuthTransport struct {
	serv     *service.AuthService
	validate *validator.Validate
}

func NewAuthTransport(serv *service.AuthService, validate *validator.Validate) *AuthTransport {
	return &AuthTransport{
		serv:     serv,
		validate: validate,
	}
}

type RequestDTO struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

type ResponceTokensDTO struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token,omitempty"` // не всегда нужен
	AccessExpireTime time.Time `json:"expires_in"`
	TokenType        string    `json:"token_type"`
}

func (trans *AuthTransport) RegisterHandle(w http.ResponseWriter, r *http.Request) {
	// создаем контекст для запроса, чтобы передавать вниз по цепочке
	ctx := r.Context()

	// парсим json в структуру для дальнейшей работы
	newRequest := RequestDTO{}
	if err := json.NewDecoder(r.Body).Decode(&newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Не удалось распарсить JSON"))
		return
	}
	// валидируем полученный json по тегам
	if err := trans.validate.Struct(newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Логин или пароль не соответствуют требованиям"))
		return
	}
	// если все окей, отправляем запрос в сервис для регистрации
	tokensInfo, err := trans.serv.RegisterUserService(ctx, newRequest.Email, newRequest.Password)
	if err != nil { // все предвиденные ошибки там уже должны быть обработаны, так что ни во что не оборачиваем
		tools.WriteError(w, err)
	}

	// отправляем токены клиенту
	newResponse := ResponceTokensDTO{
		AccessToken:      tokensInfo.AccessToken,
		RefreshToken:     tokensInfo.RefreshToken,
		AccessExpireTime: tokensInfo.AccessExpireTime,
		TokenType:        "Bearer",
	}

	// записываем ответ с токенами пользователю
	tools.WriteJSON(w, http.StatusCreated, newResponse)

}
