package transport

import (
	"accelerator/internal/core/config"
	"accelerator/internal/core/error_type"
	"accelerator/internal/core/server/authctx"
	"accelerator/internal/domains"
	"bytes"
	"context"
	"log/slog"
	"strconv"

	"accelerator/internal/core/storage"
	"accelerator/internal/features/tasks/service"
	"accelerator/internal/features/tasks/transport/dto"
	"accelerator/internal/tools"
	"encoding/json"
	"os"

	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/h2non/filetype"
)

type TasksTransport struct {
	serv      *service.TasksService
	minio     *storage.MinIOClient
	uploadSem chan struct{} // для ограничения количество одновременных загрузок временных файлов на диск
	validate  *validator.Validate
	cfg       *config.Config
}

func NewTasksTransport(serv *service.TasksService, minio *storage.MinIOClient, uploadSem chan struct{}, validate *validator.Validate, cfg *config.Config) *TasksTransport {
	return &TasksTransport{
		serv:      serv,
		minio:     minio,
		uploadSem: uploadSem,
		validate:  validate,
		cfg:       cfg,
	}
}

// ======================================== ЗАГРУЗКА АУДИО И СОЗДАНИЕ ЗАДАЧИ ==========================================

// POST api/v1/tasks/upload/{groupID}
func (trans *TasksTransport) UploadHandle(w http.ResponseWriter, r *http.Request) {

	// ------------------------------------> СОЗДАНИЕ КОНТЕКСТА И ВАЛИДАЦИЯ ID <------------------------------------------
	ctx := r.Context()

	// получаем id пользователя из токена
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	groupID := chi.URLParam(r, "groupID")
	if err := trans.validate.Struct(dto.GroupIDRequestDTO{GroupID: groupID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID группы"))
		return
	}

	// ----------------------------------------> ВАЛИДАЦИЯ РАЗМЕРА ЗАПРОСА <------------------------------------------------

	// максимальный размер обрабатываемого аудио из конфига, преобразуем в байты
	maxFileSize := int64(trans.cfg.SizeLimitAudioMB) * 1024 * 1024
	// Ограничиваем общий размер запроса (файл + 10 МБ на служебные поля)
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+10*1024*1024)

	// -----------------------------> ПАРСИМ MULTIPART DATA, ВАЛИДИРУЕМ АУДИО И JSON <----------------------------------------
	// ПРИ ЗАПРОСЕ ОБЯЗАТЕЛЬНО СНАЧАЛА DATA ПОТОМ AUDIO
	// https://chat.deepseek.com/share/roav1gjcsw30tm7pkw <<--------  объяснение, я уже ничего не соображаю

	// создаем stream-парсер запроса
	// это позволит читать части по мере их поступления от клиента
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var newRequest dto.RequestUploadDTO
	var originalFilename string
	var filePart *multipart.Part
	// флаги для досрочного выхода и чтобы кинуть ошибку, если одной из частей не будет в запросе
	jsonFound := false
	fileFound := false
	// ищем часть с нужным именем поля для аудио
	for {
		part, err := reader.NextPart()
		if err == io.EOF { // если закончились поля
			break
		}
		if err != nil { // если же случилась ошибка при чтении part
			// Если ошибка вызвана превышением лимита MaxBytesReader, err будет содержать "http: request body too large"
			if strings.Contains(err.Error(), "too large") {
				tools.WriteError(w, error_type.NewBadRequest( // кидаем ошибку пользователю с валидным размером файла
					fmt.Sprintf("Размер запроса превышает лимит (максимум %d MB)", trans.cfg.SizeLimitAudioMB),
				))
				return
			}
			// другая ошибка чтения
			tools.WriteError(w, error_type.NewBadRequest("Ошибка при чтении файла"))
			return
		}

		// ищем данные по ключам, которые указали при запросе
		if part.FormName() == "audio" { // когда нашли нужное поле, получаем полное название и сам part
			filePart = part                    // !!!!!!!!!!!!!!!!!!!!!!!!!!
			originalFilename = part.FileName() // !!!!!!!!!!!!!!!!!!!!!!!!!!
			fileFound = true
		}
		if part.FormName() == "data" { // нашли json, валидируем его
			buf, err := io.ReadAll(part)
			if err != nil {
				tools.WriteError(w, error_type.NewBadRequest("Ошибка чтения JSON"))
				return
			}
			if err := json.Unmarshal(buf, &newRequest); err != nil {
				tools.WriteError(w, error_type.NewBadRequest("Неверный формат JSON"))
				return
			}
			if err := trans.validate.Struct(newRequest); err != nil {
				tools.WriteError(w, error_type.NewBadRequest("Ошибка валидации JSON"))
				return
			}
			jsonFound = true
		}

		// если все нашли раньше окончания цикла, выходим досрочно
		if jsonFound && fileFound {
			break
		}
	}

	// пользователь не загрузил аудио файл или не передал json
	if !jsonFound || !fileFound {
		tools.WriteError(w, error_type.NewBadRequest("Отсутствует data или audio"))
		return
	}
	defer filePart.Close() // закрываем после завершения работы, чтобы избежать утечки данных

	// ---------------------------------------> ОСНОВНАЯ ВАЛИДАЦИЯ АУДИО <-----------------------------------------------
	// cоздаём буфер head размером 512 байт. 512 - это стандартное количество байт, достаточное для определения типа большинства файлов
	const signatureSize = 512 // эта библиотека - filetype - определяеет тип файла по каким-то числам в байтах
	head := make([]byte, signatureSize)

	// будем читать эти байты из filePart, представляющий тело загружаемого файла, будет читать, пока не закончатся байты или закончится файл
	// возвращает реальное количество прочитанных байт
	n, err := io.ReadFull(filePart, head)
	// если получили ошибку чтения и при этом это не ошибка, означающая, что файл закончился, если файл очень маленького размера
	if err != nil && err != io.ErrUnexpectedEOF {
		tools.WriteError(w, error_type.NewBadRequest("Ошибка при чтении сигнатуры файла"))
		return
	}

	// создаем новый head на случай, если файл маленький и прочиталось меньше 512 б
	head = head[:n]
	// на всякий случай проверим, поттому что непонятно, как отреагирует filetype.IsAudio на пустой слайс
	if n == 0 {
		tools.WriteError(w, error_type.NewBadRequest("Файл пуст"))
		return
	}
	// проверяем MIME-тип загружаемого файла, если не аудио, кидаем ошибку
	// сравнивает сигнатуры с известными аудиоформатами MP3, WAV, OGG, FLAC, M4A ...
	if !filetype.IsAudio(head) {
		tools.WriteError(w, error_type.NewBadRequest("Неподдерживаемый тип файла"))
		return
	}
	// возвращает структуру Type, содержащую поля: MIME (например, "audio/mpeg")
	kind, err := filetype.Match(head)
	if err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Не удалось определить тип файла"))
		return
	}
	fileType := kind.MIME.Value // !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!

	// ---------------------------------------> ВАЛИДАЦИЯ НА ФОРМАТ <-----------------------------------------------
	// сразу проверяем на валидный формат
	isValidType := false
	validTypes := [5]string{"audio/mpeg", "audio/x-wav", "audio/ogg", "audio/aac", "audio/x-flac"}
	for _, validType := range validTypes {
		if validType == fileType {
			isValidType = true
		}
	}
	if !isValidType {
		tools.WriteError(w, error_type.NewBadRequest("Неподдерживаемый тип аудиофайла"))
		return
	}

	// Объединяем head с оставшимся потоком
	fullStream := io.MultiReader(bytes.NewReader(head), filePart)

	// ------------------------------> ДАННЫЕ О РАБОТЕ ГОРУТИН И ОЧИСТКЕ ВРЕМЕННОГО ФАЙЛА <------------------------------
	var (
		goroutineStarted bool
		tmpFile          *os.File
	)

	// -----------------------------------> СОХРАНЯЕМ ВО ВРЕМЕННЫЙ ФАЙЛ <-----------------------------------------------
	// ограничиваем максимальное количество записей, чтобы не перезагрузить диск
	select {
	case trans.uploadSem <- struct{}{}:
		// если нетт, то здесь заблокируемся
	default:
		// если все воркеры заняты, можно ответить 503
		tools.WriteError(w, error_type.NewServiceUnavailable("Слишком много загрузок, попробуйте позже"))
		return
	}

	// Если случится ошибка до запуска горутины – освободим семафор и очистим временный файл
	defer func() {
		if !goroutineStarted {
			<-trans.uploadSem // освобождаем семафор
			if tmpFile != nil {
				tmpFile.Close()
				os.Remove(tmpFile.Name())
			}
		}
	}()

	// основная логика загрузки в файл
	tmpFile, err = os.CreateTemp("", "upload-*.tmp")
	if err != nil {
		tools.WriteError(w, error_type.NewInternal(fmt.Errorf("не удалось создать временный файл: %w", err)))
		return
	}

	// копируем данные из filePart в tmpFile, контролируя размер
	written, err := io.CopyN(tmpFile, fullStream, maxFileSize+1)
	if err != nil && err != io.EOF {
		tools.WriteError(w, error_type.NewBadRequest("Ошибка при сохранении файла"))
		return
	}
	if written > maxFileSize {
		tools.WriteError(w, error_type.NewBadRequest(
			fmt.Sprintf("Размер файла превышает лимит (максимум %d MB)", trans.cfg.SizeLimitAudioMB),
		))
		return
	}
	// перематываем временный файл на начало для всех последующих чтений
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		tools.WriteError(w, error_type.NewInternal(fmt.Errorf("ошибка перемотки файла: %w", err)))
		return
	}

	// -----------------------------> ЗАГРУЖАЕМ МЕТАДАННЫЕ В БД И РЕГИСТРИРУЕМ ЗАДАЧУ <-----------------------------------
	// передаем туда все полученные данные и ссылку на файл в хранилище, получаем информацию на клиент

	// глобальная перемена для возврата и для горутины
	taskID := uuid.New().String()
	// сохраняем objectKey в БД в поле file_path
	objectKey := config.UploadKey(groupID, taskID)
    
	// создает задачу с сгенерированным заранее ID
	taskInfo, err := trans.serv.UploadTaskService(
		ctx,
		taskID, callerID, groupID,
		newRequest.TaskName, newRequest.Description, newRequest.MeetingDate, newRequest.PatternID,
		originalFilename, objectKey, string(domains.StatusProcessingUpload),
	)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// -----------------------------> ЗАПУСКАЕМ ГОРУТИНУ, ЧТОБЫ НЕ ЗАДЕРЖИВАТЬ КЛИЕНТ <---------------------------------------------
	goroutineStarted = true // чтобы defer не чистил файл и не освобождал семафор
	go func() {
		defer func() { <-trans.uploadSem }() // освобождаем семафор, если временный файл дошел до горутин без ошибок
		trans.processUpload(
			callerID,
			taskID,
			objectKey,
			fileType,
			written,
			tmpFile, // созданный прежде файл, закроется и удалится в горутине
		)
	}()

	// -----------------------------> ФОРМИРУЕМ ОТВЕТ КЛИЕНТУ <---------------------------------------------
	newResponse := dto.ResponseUploadDTO{
		TaskID:      taskID,
		Status:      taskInfo.Status,
		TaskName:    taskInfo.TaskName,
		Description: taskInfo.Description,
		MeetingDate: taskInfo.MeetingDate,
		PatternID:   taskInfo.PatternID,

		FileName:  originalFilename,
		FileType:  fileType,
		CreatedAt: taskInfo.CreatedAt,

		ChangeFlag: taskInfo.ChangeFlag,
	}

	tools.WriteJSON(w, http.StatusCreated, newResponse)

}

// вспомогательная функция для асинхронной обработки
func (trans *TasksTransport) processUpload(
	callerID, taskID, objectKey, fileType string,
	fileSize int64,
	tmpFile *os.File,
) {
	// Собственный контекст с таймаутом (например, 30 минут на загрузку)
	ctx, cancel := context.WithTimeout(context.Background(), trans.cfg.LimitUploadAudioMinutes)
	defer cancel()

	// удаляем и закрываем файл при завершении горутины
	defer tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// defer для обновления статуса в любом случае
	// переменные, чтобы вызывать их в дефере, если нет ошибки
	var taskErr error
	var duration int
	defer func() {
		if taskErr != nil {
			slog.Error("Ошибка фонового воркера processUpload:", "err", taskErr)
			if err := trans.serv.UpdateTaskStatusService(ctx, callerID, taskID, string(domains.StatusErrorUpload)); err != nil {
				slog.Error("Не удалось обновить статус задачи", "taskID", taskID, "error", err)
			}
		} else {
			// когда успешно вычислятся длительность и загрузится в s3, тогда добавляем ссылку в бд и длительность
			// также обновляем статус задачи, ожидает деноизинга

			if err := trans.serv.UpdateTaskSuccessUploadService(
				ctx, callerID, taskID, objectKey,
				duration, string(domains.StatusPendingDenoise),
			); err != nil {
				slog.Error("Не удалось обновить статус задачи после успешной загрузки", "taskID", taskID, "error", err)
				// Также можно попытаться перевести в error
				_ = trans.serv.UpdateTaskStatusService(ctx, callerID, taskID, string(domains.StatusErrorUpload))
			}
		}
	}()

	// --------------------------------------> ПОЛУЧАЕМ ДЛИТЕЛЬНОСТЬ АУДИО <---------------------------------------------
	// Перемотка и определение длительности
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		taskErr = fmt.Errorf("перемотка: %w", err)
		return
	}

	switch fileType {
	case "audio/mpeg":
		duration, taskErr = tools.GetDurationFromMP3(tmpFile)
	case "audio/x-wav":
		duration, taskErr = tools.GetDurationFromWAV(tmpFile)
	case "audio/ogg":
		duration, taskErr = tools.GetDurationFromOGG(tmpFile)
	case "audio/aac":
		duration, taskErr = tools.GetDurationFromAAC(tmpFile)
	case "audio/x-flac":
		duration, taskErr = tools.GetDurationFromFLAC(tmpFile)
	}
	if taskErr != nil {
		taskErr = fmt.Errorf("определение длительности: %w", taskErr)
		return
	}

	// --------------------------------------> ЗАГРУЖАЕМ В S3 ХРАНИЛИЩЕ <---------------------------------------------
	// Перемотка и загрузка в S3
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		taskErr = fmt.Errorf("перемотка перед S3: %w", err)
		return
	}

	_, err := trans.minio.UploadFile(ctx, objectKey, tmpFile, fileSize, fileType)
	if err != nil {
		taskErr = fmt.Errorf("загрузка в S3: %w", err)
		return
	}
}

// =========================================== ПОЛУЧЕНИЕ СТАТУСА ЗАДАЧИ ==========================================

// GET api/v1/tasks/{taskID}/audio
func (trans *TasksTransport) GetAudioTaskHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if err := trans.validate.Struct(dto.TaskIDRequestDTO{TaskID: taskID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID задачи"))
		return
	}

	// получаем всю нужную информацию о статусе задачи
	audioURL, expiresAt, err := trans.serv.GetAudioTaskHandle(ctx, callerID, taskID)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим результат в дто и отправляем на клиент
	newResponse := dto.AudioResponseDTO{
		AudioURL: audioURL,
		ExpiresAt: expiresAt,
	}

	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// =========================================== ПОЛУЧЕНИЕ СТАТУСА ЗАДАЧИ ==========================================

// GET api/v1/tasks/{taskID}/status
func (trans *TasksTransport) CheckStatusTaskHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if err := trans.validate.Struct(dto.TaskIDRequestDTO{TaskID: taskID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID задачи"))
		return
	}

	// получаем всю нужную информацию о статусе задачи
	taskCheckInfo, err := trans.serv.GetTaskStatusService(ctx, callerID, taskID)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим результат в дто и отправляем на клиент
	newResponse := dto.ResponseCheckTaskDTO{
		Status:                     taskCheckInfo.Status,
		IsProcess:                  taskCheckInfo.IsProcess,
		InTheQueueBefore:           taskCheckInfo.InTheQueueBefore,
		ApproximateLeadTimeProcess: taskCheckInfo.ApproximateLeadTimeProcess,
	}

	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// ======================================== ПОЛУЧЕНИЕ РЕЗУЛЬТАТОВ ЗАДАЧИ ==========================================

// GET api/v1/tasks/{taskID}
func (trans *TasksTransport) GetTaskHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if err := trans.validate.Struct(dto.TaskIDRequestDTO{TaskID: taskID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID задачи"))
		return
	}

	// получаем всю нужную информацию о задаче
	taskInfo, err := trans.serv.GetTaskService(ctx, callerID, taskID)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим результат в дто и отправляем на клиент
	newResponse := dto.ResponseTaskDTO{
		TaskID:  taskInfo.TaskID,
		UserID:  taskInfo.UserID,
		GroupID: taskInfo.GroupID,

		TaskName:    taskInfo.TaskName,
		Description: taskInfo.Description,
		MeetingDate: taskInfo.MeetingDate,
		PatternID:   taskInfo.PatternID,

		Status:     taskInfo.Status,
		ResultJson: taskInfo.ResultJson,

		FileName: taskInfo.FileName,
		Duration: taskInfo.Duration,

		CreatedAt:   taskInfo.CreatedAt,
		UpdatedAt:   taskInfo.UpdatedAt,
		StartedAt:   taskInfo.StartedAt,
		CompletedAt: taskInfo.CompletedAt,

		ChangeFlag: taskInfo.ChangeFlag,
	}

	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// ===================================== ПОЛУЧЕНИЕ ВСЕХ ЗАДАЧ В ГРУППЕ ==========================================

// GET api/v1/tasks/{groupID}/all
func (trans *TasksTransport) GetAllTaskInGroupHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	groupID := chi.URLParam(r, "groupID")
	if err := trans.validate.Struct(dto.GroupIDRequestDTO{GroupID: groupID}); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("некорректный ID группы"))
		return
	}

	// получаем параметры пагинации из query параметров
	newRequest := dto.PaginationRequestDTO{
		Page:  r.URL.Query().Get("page"),
		Limit: r.URL.Query().Get("limit"),
	}

	if err := trans.validate.Struct(newRequest); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("Ошибка во входных данных"))
		return
	}

	// переводим данные в integer, уже валидировали, так что ошибку не получаем, там точно int
	pageInt, _ := strconv.Atoi(newRequest.Page)
	limitInt, _ := strconv.Atoi(newRequest.Limit)

	// получаем список всех задач с пагинацией или же без, если -1
	tasksInfo, totalTasks, err := trans.serv.GetAllTaskInGroupService(
		ctx, callerID, groupID,
		pageInt, limitInt,
	)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим в массив и отправляем на клиент
	var newResponseTasks []dto.ResponseTaskDTO

	for _, task := range *tasksInfo {
		newResponseTask := dto.ResponseTaskDTO{
			TaskID:  task.TaskID,
			UserID:  task.UserID,
			GroupID: task.GroupID,

			TaskName:    task.TaskName,
			Description: task.Description,
			MeetingDate: task.MeetingDate,
			PatternID:   task.PatternID,

			Status:     task.Status,
			ResultJson: task.ResultJson,

			FileName: task.FileName,
			Duration: task.Duration,

			CreatedAt:   task.CreatedAt,
			UpdatedAt:   task.UpdatedAt,
			StartedAt:   task.StartedAt,
			CompletedAt: task.CompletedAt,

			ChangeFlag: task.ChangeFlag,
		}

		newResponseTasks = append(newResponseTasks, newResponseTask)
	}
	newResponsePagination := dto.PaginationResponseDTO{
		Page:  pageInt,
		Limit: limitInt,
		Total: totalTasks,
	}

	newResponse := dto.AllTasksResponseDTO{
		Tasks:      newResponseTasks,
		Pagination: newResponsePagination,
	}

	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// ===================================== ИЗМЕНЕНИЕ ЗАДАЧИ ==========================================

// PUT api/v1/tasks/{taskID}
func (trans *TasksTransport) EditTaskHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	// валидируем ID задачи
	newRequestTaskID := dto.TaskIDRequestDTO{
		TaskID: chi.URLParam(r, "taskID"),
	}

	// получаем и валидируем данные для изменения от пользователя
	var req dto.EditTaskRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("не удалось распарсить json"))
		return
	}
	if err := trans.validate.Struct(req); err != nil {
		tools.WriteError(w, error_type.NewBadRequest("ошибка во входных данных"))
		return
	}

	// собираем ответ в мапу, пустые значения обрабатываем
	updateData := make(map[string]string)
	if req.TaskName != nil {
		if *req.TaskName == "" {
			tools.WriteError(w, error_type.NewBadRequest("название для задачи не может быть пустым"))
			return
		}
		updateData["task_name"] = *req.TaskName
	}
	if req.Description != nil {
		if *req.Description == "" {
			tools.WriteError(w, error_type.NewBadRequest("описание для задачи не может быть пустым"))
			return
		}
		updateData["description"] = *req.Description
	}
	if req.MeetingDate != nil {
		if *req.MeetingDate == "" {
			tools.WriteError(w, error_type.NewBadRequest("дата не может быть пустой"))
			return
		}
		updateData["meeting_date"] = *req.MeetingDate
	}
	if len(updateData) == 0 {
		tools.WriteError(w, error_type.NewBadRequest("не передано ни одного поля для изменения"))
		return
	}

	updatedTask, err := trans.serv.EditTaskService(ctx, callerID, newRequestTaskID.TaskID, updateData)
	if err != nil {
		tools.WriteError(w, err)
		return
	}

	// маппим результат в дто и отправляем на клиент
	newResponse := dto.ResponseTaskDTO{
		TaskID:  updatedTask.TaskID,
		UserID:  updatedTask.UserID,
		GroupID: updatedTask.GroupID,

		TaskName:    updatedTask.TaskName,
		Description: updatedTask.Description,
		MeetingDate: updatedTask.MeetingDate,
		PatternID:   updatedTask.PatternID,

		Status:     updatedTask.Status,
		ResultJson: updatedTask.ResultJson,

		FileName: updatedTask.FileName,
		Duration: updatedTask.Duration,

		CreatedAt:   updatedTask.CreatedAt,
		UpdatedAt:   updatedTask.UpdatedAt,
		StartedAt:   updatedTask.StartedAt,
		CompletedAt: updatedTask.CompletedAt,

		ChangeFlag: updatedTask.ChangeFlag,
	}

	tools.WriteJSON(w, http.StatusOK, newResponse)
}

// ===================================== УДАЛЕНИЕ ЗАДАЧИ ===========================================
// DELETE api/v1/tasks/{taskID}
func (trans *TasksTransport) DeleteTaskHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerID, ok := authctx.GetUserID(ctx)
	if !ok {
		tools.WriteError(w, error_type.NewUnauthorized("missing authentication context"))
		return
	}

	newRequestTaskID := dto.TaskIDRequestDTO{
		TaskID: chi.URLParam(r, "taskID"),
	}

	if err := trans.serv.DeleteTaskService(ctx, callerID, newRequestTaskID.TaskID); err != nil {
		tools.WriteError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
