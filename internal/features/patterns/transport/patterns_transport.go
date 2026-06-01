package transport

import (
	"accelerator/internal/core/error_type"
	"accelerator/internal/core/server/authctx"
	"accelerator/internal/features/patterns/service"
	"accelerator/internal/features/patterns/transport/dto"
	"accelerator/internal/tools"
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

type PatternsTransport struct {
	serv     *service.PatternsService
	validate *validator.Validate
}

func NewPatternsTransport(serv *service.PatternsService, validate *validator.Validate) *PatternsTransport {
	return &PatternsTransport{
		serv:     serv,
		validate: validate,
	}
}

// ============================== СОЗДАНИTЕ НОВОГО ШАБЛОНА ==============================
// POST patterns
// если креатор укажет group_id в дто, то он создаст локальный шаблон для группы
// если не укажет, то он будет глобальный
func (trans *PatternsTransport) CreatePatternHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// получение переменных из контекста
	callerID, okID := authctx.GetUserID(ctx)
	callerRole, okRole := authctx.GetUserRole(ctx)
	if !okID || !okRole {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}
	// и первичная проверка доступа
	if callerRole != "creator" && callerRole != "admin" {
		tools.WriteError(w, error_type.NewNotFound("Страница не найдена"))
		return
	}

	// получаем ДТО и валидируем от пользователя
	var newRequest dto.CreatePatternRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("не удалось распарсить json"))
		return
	}
	if err := trans.validate.Struct(newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("ошибка во входных данных"))
		return
	}

	if len(newRequest.AdditionalPrompt) == 0 || bytes.Equal(bytes.TrimSpace(newRequest.AdditionalPrompt), []byte("null")) {
		newRequest.AdditionalPrompt = nil
	} else {
		// если передавали, проверяем, что пришел валидный json в additional_prompt
		var tmp interface{}
		if err := json.Unmarshal(newRequest.AdditionalPrompt, &tmp); err != nil {
			tools.WriteError(w, error_type.NewBadRequest("невалидный json в additional_prompt"))
			return
		}
		// Разрешаем только объект или массив
		switch v := tmp.(type) {
		case map[string]interface{}:
			// нормализуем пустой объект {} до nil, чтобы в бд было NULL
			if len(v) == 0 {
				newRequest.AdditionalPrompt = nil // пустой объект -> NULL
			}
		case []interface{}:
			// нормализуем пустой объект [] до nil, чтобы в бд было NULL
			if len(v) == 0 {
				newRequest.AdditionalPrompt = nil // пустой массив -> NULL
			}
		default:
			tools.WriteError(w, error_type.NewBadRequest("additional_prompt должен быть массивом [] или null"))
			return
		}

		// если json был не пустой, проверяем, что он соответствует нужному виду
		if newRequest.AdditionalPrompt != nil {
			if err := tools.ValidateAdditionalPrompt(newRequest.AdditionalPrompt); err != nil {
				tools.WriteError(w, error_type.NewBadRequest(err.Error()))
				return
			}
		}

	}

	// вызыв сервиса для создания шаблона
	newPattern, err := trans.serv.CreatePatternService(
		ctx, callerID, newRequest.GroupID, newRequest.Name, newRequest.Description, newRequest.SummaryPrompt, newRequest.AdditionalPrompt,
	)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим в PatternResponseDTO и отправляем на клиент
	newResponse := dto.PatternResponseDTO{
		ID:               newPattern.ID,
		GroupID:          newPattern.GroupID,
		Name:             newPattern.Name,
		Description:      newPattern.Description,
		SummaryPrompt:    newPattern.SummaryPrompt,
		AdditionalPrompt: newPattern.AdditionalPrompt,
		CreatedAt:        newPattern.CreatedAt,
		ChangeFlag:       newPattern.ChangeFlag,
	}

	tools.WriteJSON(w, http.StatusCreated, newResponse)
}

// ================================= ПОЛУЧЕНИЕ ШАБЛОНА =================================
// GET patterns/{patternID}
func (trans *PatternsTransport) GetPattern(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	// получение и валидация pattern ID
	patternID := chi.URLParam(r, "patternID")
	if err := trans.validate.Struct(dto.PatternIDDTO{PatternID: patternID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID шаблона"))
		return
	}

	// получаем конкретный шаблон, если он есть, ограничиваем доступ к групповым шаблонам, если user или admin не состоят в группе
	patternInfo, err := trans.serv.GetPattern(ctx, callerID, patternID)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	newResponse := dto.PatternResponseDTO{
		ID:               patternInfo.ID,
		GroupID:          patternInfo.GroupID,
		Name:             patternInfo.Name,
		Description:      patternInfo.Description,
		SummaryPrompt:    patternInfo.SummaryPrompt,
		AdditionalPrompt: patternInfo.AdditionalPrompt,
		CreatedAt:        patternInfo.CreatedAt,
		ChangeFlag:       patternInfo.ChangeFlag,
	}

	tools.WriteJSON(w, http.StatusOK, newResponse)

}

// ======================= ПОЛУЧЕНИЕ ВСЕХ ШАБЛОНОВ, ДОСТУПНЫХ В ГРУППЕ С ФЛАГАМИ =========================
// GET patterns/all/{groupID}
func (trans *PatternsTransport) GetGroupPatterns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	// получение и валидация group ID
	groupID := chi.URLParam(r, "groupID")
	if err := trans.validate.Struct(dto.GroupIDDTO{GroupID: groupID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID группы"))
		return
	}

	globalPatternsInfo, groupPatternsInfo, err := trans.serv.GetPatternsInGroupAndGlobal(ctx, callerID, groupID)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим в массив дто глобальные
	var newGlobalPatternsResponse []dto.PatternResponseDTO

	for i := 0; i < len(*globalPatternsInfo); i++ {
		newGlobalPatternResponse := dto.PatternResponseDTO{
			ID:               (*globalPatternsInfo)[i].ID,
			GroupID:          (*globalPatternsInfo)[i].GroupID,
			Name:             (*globalPatternsInfo)[i].Name,
			Description:      (*globalPatternsInfo)[i].Description,
			SummaryPrompt:    (*globalPatternsInfo)[i].SummaryPrompt,
			AdditionalPrompt: (*globalPatternsInfo)[i].AdditionalPrompt,
			CreatedAt:        (*globalPatternsInfo)[i].CreatedAt,
			ChangeFlag:       (*globalPatternsInfo)[i].ChangeFlag,
		}
		newGlobalPatternsResponse = append(newGlobalPatternsResponse, newGlobalPatternResponse)
	}

	// маппим в массив дто групповые
	var newGroupPatternsResponse []dto.PatternResponseDTO

	for i := 0; i < len(*groupPatternsInfo); i++ {
		newGroupPatternResponse := dto.PatternResponseDTO{
			ID:               (*groupPatternsInfo)[i].ID,
			GroupID:          (*groupPatternsInfo)[i].GroupID,
			Name:             (*groupPatternsInfo)[i].Name,
			Description:      (*groupPatternsInfo)[i].Description,
			SummaryPrompt:    (*groupPatternsInfo)[i].SummaryPrompt,
			AdditionalPrompt: (*groupPatternsInfo)[i].AdditionalPrompt,
			CreatedAt:        (*groupPatternsInfo)[i].CreatedAt,
			ChangeFlag:       (*groupPatternsInfo)[i].ChangeFlag,
		}
		newGroupPatternsResponse = append(newGroupPatternsResponse, newGroupPatternResponse)
	}

	newResponse := dto.GroupPatternsResponseDTO{
		GlobalPatterns: newGlobalPatternsResponse,
		GroupPatterns:  newGroupPatternsResponse,
	}

	// записываем
	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// ====================== ПОЛУЧЕНИЕ СВОИХ ШАБЛОНОВ ДЛЯ КРЕАТОРА ========================
// GET patterns/global
func (trans *PatternsTransport) GetCreatorPatterns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// получение переменных из контекста
	callerID, okID := authctx.GetUserID(ctx)
	callerRole, okRole := authctx.GetUserRole(ctx)
	if !okID || !okRole {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}
	// и первичная проверка доступа
	if callerRole != "creator" {
		tools.WriteError(w, error_type.NewNotFound("Страница не найдена"))
		return
	}

	globalPatternsInfo, err := trans.serv.GetPatternsGlobal(ctx, callerID)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим в массив дто глобальные
	var newGlobalPatternsResponse []dto.PatternResponseDTO

	for i := 0; i < len(*globalPatternsInfo); i++ {
		newGlobalPatternResponse := dto.PatternResponseDTO{
			ID:               (*globalPatternsInfo)[i].ID,
			GroupID:          (*globalPatternsInfo)[i].GroupID,
			Name:             (*globalPatternsInfo)[i].Name,
			Description:      (*globalPatternsInfo)[i].Description,
			SummaryPrompt:    (*globalPatternsInfo)[i].SummaryPrompt,
			AdditionalPrompt: (*globalPatternsInfo)[i].AdditionalPrompt,
			CreatedAt:        (*globalPatternsInfo)[i].CreatedAt,
			ChangeFlag:       (*globalPatternsInfo)[i].ChangeFlag,
		}
		newGlobalPatternsResponse = append(newGlobalPatternsResponse, newGlobalPatternResponse)
	}

	newResponse := dto.GlobalPatternsResponseDTO{
		GlobalPatterns: newGlobalPatternsResponse,
	}

	// записываем
	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// ================ ПОЛУЧЕНИЕ ВСЕХ ШАБЛОНОВ ДЛЯ КРЕАТОРА ПО ГРУППАМ ====================
// GET patterns/all
func (trans *PatternsTransport) GetAllPatternsInGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// получение переменных из контекста
	callerID, okID := authctx.GetUserID(ctx)
	callerRole, okRole := authctx.GetUserRole(ctx)
	if !okID || !okRole {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}
	// и первичная проверка доступа
	if callerRole != "creator" {
		tools.WriteError(w, error_type.NewNotFound("Страница не найдена"))
		return
	}

	allPatternsInfo, err := trans.serv.GetAllPatternsInGroups(ctx, callerID)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим в массив дто глобальные
	var newAllGroupPatternsResponse []dto.GroupWithPatternsResponse

	for i := 0; i < len(*allPatternsInfo); i++ {
		var newGroupPatternsResponse []dto.PatternResponseDTO

		for j := 0; j < len(((*allPatternsInfo)[i].Patterns)); j++ {
			newGroupPatternResponse := dto.PatternResponseDTO{
				ID:               (*allPatternsInfo)[i].Patterns[j].ID,
				GroupID:          (*allPatternsInfo)[i].Patterns[j].GroupID,
				Name:             (*allPatternsInfo)[i].Patterns[j].Name,
				Description:      (*allPatternsInfo)[i].Patterns[j].Description,
				SummaryPrompt:    (*allPatternsInfo)[i].Patterns[j].SummaryPrompt,
				AdditionalPrompt: (*allPatternsInfo)[i].Patterns[j].AdditionalPrompt,
				CreatedAt:        (*allPatternsInfo)[i].Patterns[j].CreatedAt,
				ChangeFlag:       (*allPatternsInfo)[i].Patterns[j].ChangeFlag,
			}
			newGroupPatternsResponse = append(newGroupPatternsResponse, newGroupPatternResponse)
		}

		newAllGroupPatternResponse := dto.GroupWithPatternsResponse{
			GroupID:     (*allPatternsInfo)[i].GroupID,
			Name:        (*allPatternsInfo)[i].Name,
			Description: (*allPatternsInfo)[i].Description,
			Patterns:    newGroupPatternsResponse,
		}

		newAllGroupPatternsResponse = append(newAllGroupPatternsResponse, newAllGroupPatternResponse)
	}

	newResponse := dto.AllGroupWithPatternsResponse{
		Groups: newAllGroupPatternsResponse,
	}

	// записываем
	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// =============================== ИЗМЕНЕНИЕ ШАБЛОНА ===================================
// PUT patterns/{patternID}
func (trans *PatternsTransport) EditPattern(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// получение переменных из контекста
	callerID, okID := authctx.GetUserID(ctx)
	callerRole, okRole := authctx.GetUserRole(ctx)
	if !okID || !okRole {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}
	// и первичная проверка доступа
	if callerRole != "creator" && callerRole != "admin" {
		tools.WriteError(w, error_type.NewNotFound("Страница не найдена"))
		return
	}

	// получение и валидация pattern ID
	patternID := chi.URLParam(r, "patternID")
	if err := trans.validate.Struct(dto.PatternIDDTO{PatternID: patternID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID шаблона"))
		return
	}
	// читаем тело один раз, чтобы потом отличить отсутствие ключа от явного null
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		tools.WriteError(w, error_type.NewBadRequest("не удалось прочитать тело запроса"))
		return
	}

	// получаем ДТО и валидируем от пользователя
	var newRequest dto.EditPatternRequestDTO
	if err := json.Unmarshal(bodyBytes, &newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("не удалось распарсить json"))
		return
	}
	if err := trans.validate.Struct(newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("ошибка во входных данных"))
		return
	}

	// какие ключи реально пришли в запросе (нужно, чтобы явный null/[]/{}
	// очищал additional_prompt, а отсутствие ключа — нет)
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &rawFields); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("не удалось распарсить json"))
		return
	}

	// собираем только данные, которые не равны пустой строке или nil и которые нужно изменить
	// если пустые строки, то кидаем ошибку, что поле не может быть пустым
	updateData := make(map[string]any)

	if newRequest.Name != nil {
		if *newRequest.Name == "" {
			tools.WriteError(w, error_type.NewBadRequest("название шаблона не может быть пустой строкой"))
			return
		}
		updateData["name"] = *newRequest.Name
	}
	// может быть пустой строкой, ничего страшного
	if newRequest.Description != nil {
		updateData["description"] = *newRequest.Description
	}
	// Должность, а здесь проверяем
	if newRequest.SummaryPrompt != nil {
		if *newRequest.SummaryPrompt == "" {
			tools.WriteError(w, error_type.NewBadRequest("промпт не может быть пустой строкой"))
			return
		}
		updateData["summary_prompt"] = *newRequest.SummaryPrompt
	}
	// additional_prompt обрабатываем, ТОЛЬКО если ключ реально присутствует в запросе.
	// Явный null / [] / {} -> очищаем поле (NULL в БД).
	if raw, ok := rawFields["additional_prompt"]; ok {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			updateData["additional_prompt"] = nil // null -> NULL в БД
		} else {
			// если передавали, проверяем, что пришел валидный json в additional_prompt
			var tmp interface{}
			if err := json.Unmarshal(raw, &tmp); err != nil {
				tools.WriteError(w, error_type.NewBadRequest("невалидный json в additional_prompt"))
				return
			}
			// Разрешаем только объект или массив
			switch v := tmp.(type) {
			case map[string]interface{}:
				// пустой объект {} -> NULL
				if len(v) == 0 {
					updateData["additional_prompt"] = nil
				} else {
					tools.WriteError(w, error_type.NewBadRequest("additional_prompt должен быть массивом [] или null"))
					return
				}
			case []interface{}:
				if len(v) == 0 {
					// пустой массив [] -> NULL
					updateData["additional_prompt"] = nil
				} else {
					// непустой массив — проверяем структуру и записываем как есть
					if err := tools.ValidateAdditionalPrompt(raw); err != nil {
						tools.WriteError(w, error_type.NewBadRequest(err.Error()))
						return
					}
					updateData["additional_prompt"] = raw
				}
			default:
				tools.WriteError(w, error_type.NewBadRequest("additional_prompt должен быть массивом [] или null"))
				return
			}
		}
	}

	// Проверка, что есть что обновлять
	if len(updateData) == 0 {
		tools.WriteError(w, error_type.NewBadRequest("нет ни одного переданного аргумента для изменения"))
		return
	}

	// вызываем репозиторий, чтобы созранить изменения
	editPattern, err := trans.serv.EditPattern(ctx, callerID, patternID, updateData)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим и отправляем на клиент
	newResponse := dto.PatternResponseDTO{
		ID:               editPattern.ID,
		GroupID:          editPattern.GroupID,
		Name:             editPattern.Name,
		Description:      editPattern.Description,
		SummaryPrompt:    editPattern.SummaryPrompt,
		AdditionalPrompt: editPattern.AdditionalPrompt,
		CreatedAt:        editPattern.CreatedAt,
		ChangeFlag:       editPattern.ChangeFlag,
	}

	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// ============================= УДАЛЕНИЕ ШАБЛОНА =====================================
// DELETE patterns/{patternID}
func (trans *PatternsTransport) DeletePattern(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// получение переменных из контекста
	callerID, okID := authctx.GetUserID(ctx)
	callerRole, okRole := authctx.GetUserRole(ctx)
	if !okID || !okRole {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}
	// и первичная проверка доступа
	if callerRole != "creator" && callerRole != "admin" {
		tools.WriteError(w, error_type.NewNotFound("Страница не найдена"))
		return
	}

	// получение и валидация pattern ID
	patternID := chi.URLParam(r, "patternID")
	if err := trans.validate.Struct(dto.PatternIDDTO{PatternID: patternID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID шаблона"))
		return
	}

	// вызываем репоиторий для удаления шаблона
	err := trans.serv.DeletePattern(ctx, callerID, patternID)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)

}
