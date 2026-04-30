package service

import (
	"accelerator/internal/core/config"
	"accelerator/internal/core/error_type"
	"accelerator/internal/domains"
	"accelerator/internal/features/auth/repository"
	"accelerator/internal/tools"
	"context"
	"fmt"
)

type AuthService struct {
	repo *repository.AuthRepo
	cfg  *config.Config
}

func NewAuthService(repo *repository.AuthRepo, cfg *config.Config) *AuthService {
	return &AuthService{
		repo: repo,
		cfg:  cfg,
	}
}

// получает почту и пароль
// генерирует хэш, сохраняет пользователя, создает сессию, обрабатывает ошибки
// возвращает токены и время жизни
func (serv *AuthService) RegisterUserService(
	ctx context.Context,
	email string,
	password string,
) (*domains.ReturnCreateTokensInfo, error) {
	// ошибки от репозитория обрабатываются в нем же через стандартизированные ошибки и сразу передаются на уровень выше
	// остальные ошибки из сторонных файлов обрабатываются уже здесь

	// нужно реализовать механизм транзакции, чтобы не было сценария, когда на одном из этапов произошла ошибка, а данные в бд уже изменены
	// и например у нас получается зарегистированный пользователь без сессии, которому потом надо логиниться чтобы получить токены 
	


	// получаем хэш пароля
	password_hash, err := tools.GenerateHash(password) // оборачиваем в errorf чтобы сохранить доп информацию для логов в ошибке также сохраняем возможность проверить ее тип через Is
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("hash password: %w", err))
	}

	// создаем пользователя, получаем id
	userID, err := serv.repo.CreateUser(ctx, email, password_hash)
	if err != nil {
		return nil, err
	}

	// генерируем jwt токены, принимаем всю информацию о них
	tokensInfo, err := tools.GenerateJWTToken(userID, serv.cfg)
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("generate tokens: %w", err))
	}

	// получаем хэш refresh токена для хранения в базе данных сессий пользователя
	refreshTokenHash, err := tools.GenerateHash(tokensInfo.RefreshToken)
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("token hash: %w", err))
	}

	// создаем сессию пользователя
	err = serv.repo.CreateSession(ctx, refreshTokenHash, tokensInfo.CreateTime, tokensInfo.RefreshExpireTime)
	if err != nil {
		return nil, err
	}

	// получаем нужную информацию из модели создания токена и возвращаем токены на клиент
	return tokensInfo, nil

}
