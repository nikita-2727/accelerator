package transport

import (
	"accelerator/internal/core/config"
	"accelerator/internal/core/error_type"
	"accelerator/internal/features/tasks/service"
	"accelerator/internal/tools"
	"bytes"
	"encoding/json"

	// "encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/h2non/filetype"
)

type TasksTransport struct {
	serv     *service.TasksService
	validate *validator.Validate
	cfg      *config.Config
}

func NewTasksTransport(serv *service.TasksService, validate *validator.Validate, cfg *config.Config) *TasksTransport {
	return &TasksTransport{
		serv:     serv,
		validate: validate,
		cfg:      cfg,
	}
}

type RequestUploadDTO struct {
	TaskName         string `json:"task_name" validate:"required"`
	Description      string `json:"description"`
	MeetingDate      string `json:"meeting_date"`
	SummaryPrompt    string `json:"summary_prompt" validate:"required"`
	AdditionalPrompt string `json:"additional_prompt" validate:"required"`
	ASRModel         string `json:"asr_model" validate:"required"`
	LLMModel         string `json:"llm_model" validate:"required"`
	Tokens           string `json:"tokens" validate:"required"`
}

type ResponseUploadDTO struct {
	TaskID      string    `json:"task_id"`
	Status      string    `json:"status"`
	FileName    string    `json:"original_filename"`
	FileType    string    `json:"file_type"`
	Duration    int       `json:"duration_seconds"`
	WaitSeconds int       `json:"estimated_wait_seconds"`
	CreatedAt   time.Time `json:"created_at"`
}

func (trans *TasksTransport) UploadHandle(w http.ResponseWriter, r *http.Request) {

	// ----------------------------------------> ВАЛИДАЦИЯ РАЗМЕРА ЗАПРОСА <------------------------------------------------

	// максимальный размер обрабатываемого аудио из конфига, преобразуем в байты
	maxFileSize := int64(trans.cfg.SizeLimitAudioMB) * 1024 * 1024
	// Ограничиваем общий размер запроса (файл + 10 МБ на служебные поля)
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize + 10 * 1024 * 1024)


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

	var newRequest RequestUploadDTO
	var originalFilename string
	var filePart *multipart.Part
	// флаги для досрочного выхода и чтобы кинуть ошибку, если одной из частей не буджет в запросе
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
			filePart = part // !!!!!!!!!!!!!!!!!!!!!!!!!!
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
	defer filePart.Close() // закрываем после завершения работы, чтобы изежать утечки данных

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
	// проверяем MIME-тип загружаемого файла, если не аудио, кидаем ошибку
	// сравнивает сигнатуры с известными аудиоформатами MP3, WAV, OGG, FLAC, M4A ...
	if !filetype.IsAudio(head) {
		tools.WriteError(w, error_type.NewBadRequest("Неподдерживаемый тип файла"))
		return
	}
	// возвращает структуру Type, содержащую поля: MIME (например, "audio/mpeg")
	kind, _ := filetype.Match(head)
	fileType := kind.MIME.Value // !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!

	// объединяем прочитанные заголовки и остальной поток
	fullFileStream := io.MultiReader(bytes.NewReader(head), filePart)

	// --------------------------------------> ПОЛУЧАЕМ ДЛИТЕЛЬНОСТЬ АУДИО <---------------------------------------------
	// получаем длительность файла не скачивая его на диск напрямую из пришедшего потока в зависимости от формата
	var duration int // !!!!!!!!!!!!!!!!!!!!!!!!!!
	switch fileType {
	case "audio/mpeg":
		duration, err = tools.GetDurationFromMP3(fullFileStream)
	case "audio/x-wav":
		duration, err = tools.GetDurationFromWAV(fullFileStream)
	case "audio/ogg":
		duration, err = tools.GetDurationFromOGG(fullFileStream)
	case "audio/aac":
		duration, err = tools.GetDurationFromAAC(fullFileStream)
	case "audio/x-flac":
		duration, err = tools.GetDurationFromFLAC(fullFileStream)
	default:
		tools.WriteError(w, error_type.NewBadRequest("Неподдерживаемый тип аудиофайла"))
		return
	}
	if err != nil {
		error_type.NewInternal(fmt.Errorf("Ошибка при определении длительности файла: %w", err))
		return
	}

	// --------------------------------------> ЗАГРУЖАЕМ В S3 ХРАНИЛИЩЕ <---------------------------------------------
	// сразу стримим этот поток в s3 хранилище, не закачивая его на сервер
	// обернем fullFileStream в LimitReader для дополнительной защиты, вдруг как-то мы не распознали, что файл превышает требования,
	// просто обрежем его согласно нашему ограничению длины максимального файла, чтобы не затаймлимитить сервер
	limitedReader := io.LimitReader(fullFileStream, maxFileSize+1)
	_ = limitedReader

	// -----------------------------> ЗАГРУЖАЕМ МЕТАДАННЫЕ В БД И РЕГИСТРИРУЕМ ЗАДАЧУ <-----------------------------------
	// передаем туда все полученные данные и ссылку на файл в хранилище, получаем информацию на клиент
	// taskInfo = trans.serv.CreateTask()

	// -----------------------------> ФОРМИРУЕМ ОТВЕТ КЛИЕНТУ <---------------------------------------------
	newResponse := ResponseUploadDTO{
		TaskID:      "",
		Status:      "",
		FileName:    originalFilename,
		FileType:    fileType,
		Duration:    duration,
		WaitSeconds: 0,
		CreatedAt:   time.Now(),
	}

	tools.WriteJSON(w, http.StatusCreated, newResponse)
}
