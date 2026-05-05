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

type RequestAuthDTO struct {
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
	newRequest := RequestAuthDTO{}
	if err := json.NewDecoder(r.Body).Decode(&newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Не удалось распарсить JSON"))
		return
	}
	// валидируем полученный json по тегам
	if err := trans.validate.Struct(newRequest); err != nil {
		// получаем варианты ошибок исходя от ошибки в поле, пароль или email
		messageError := tools.GetMessageFromValidateError(err)
		tools.WriteError(w, error_type.NewBadRequest(messageError))
		return
	}
	// если все окей, отправляем запрос в сервис для регистрации
	tokensInfo, err := trans.serv.RegisterUserService(ctx, newRequest.Email, newRequest.Password)
	if err != nil { // все предвиденные ошибки там уже должны быть обработаны, так что ни во что не оборачиваем
		tools.WriteError(w, err)
		return
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



func (trans *AuthTransport) LoginHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// парсим json в структуру для дальнейшей работы
	newRequest := RequestAuthDTO{}
	if err := json.NewDecoder(r.Body).Decode(&newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Не удалось распарсить JSON"))
		return
	}
	// валидируем полученный json по тегам
	if err := trans.validate.Struct(newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Ошибка валидации"))
		return
	}
	// если все окей, отправляем запрос в сервис для входа и получения токенов
	tokensInfo, err := trans.serv.LoginUserService(ctx, newRequest.Email, newRequest.Password)
	if err != nil { 
		tools.WriteError(w, err)
		return
	}

	// отправляем токены клиенту
	newResponse := ResponceTokensDTO{
		AccessToken:      tokensInfo.AccessToken,
		RefreshToken:     tokensInfo.RefreshToken,
		AccessExpireTime: tokensInfo.AccessExpireTime,
		TokenType:        "Bearer",
	}

	// записываем ответ с токенами пользователю
	tools.WriteJSON(w, http.StatusOK, newResponse)
}



type RequestTokenDTO struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

func (trans *AuthTransport) RefreshHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// парсим json в структуру для дальнейшей работы
	newRequest := RequestTokenDTO{}
	if err := json.NewDecoder(r.Body).Decode(&newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Не удалось распарсить JSON"))
		return
	}
	// валидируем полученный json по тегам
	if err := trans.validate.Struct(newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Отсутствует токен авторизации"))
		return
	}
	// если все окей, отправляем запрос в сервис для входа и получения токенов
	tokensInfo, err := trans.serv.RefreshUserService(ctx, newRequest.RefreshToken)
	if err != nil { 
		tools.WriteError(w, err)
		return
	}

	// отправляем токены клиенту
	newResponse := ResponceTokensDTO{
		AccessToken:      tokensInfo.AccessToken,
		RefreshToken:     tokensInfo.RefreshToken,
		AccessExpireTime: tokensInfo.AccessExpireTime,
		TokenType:        "Bearer",
	}

	// записываем ответ с токенами пользователю
	tools.WriteJSON(w, http.StatusOK, newResponse)
}