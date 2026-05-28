package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"accelerator/internal/core/config"
	"accelerator/internal/core/storage"
	"accelerator/internal/domains"
	"accelerator/internal/features/tasks/repository"
	"accelerator/internal/tools"
)

type Orchestrator struct {
	tasksRepo       *repository.TasksRepo
	minio           *storage.MinIOClient
	cfg             *config.Config
	resourceManager *ResourceManager
	stages          []config.StageConfig
	httpClient      *http.Client
}

func NewOrchestrator(
	tasksRepo *repository.TasksRepo,
	minio *storage.MinIOClient,
	cfg *config.Config,
	resourceManager *ResourceManager,
	stages []config.StageConfig,
) *Orchestrator {
	return &Orchestrator{
		tasksRepo:       tasksRepo,
		minio:           minio,
		cfg:             cfg,
		resourceManager: resourceManager,
		stages:          stages,
		httpClient: &http.Client{
			Timeout: cfg.AIWorkersTimeoutHour, // длинные запросы, чтобы точно успело обработаться
		},
	}
}

// Запускает по одной горутине на каждый этап из списка stage
func (o *Orchestrator) Run(ctx context.Context) {
	for _, st := range o.stages {
		go o.runStageWorker(ctx, st) // запускаем вечный цикл для каждой
	}
}

// вечный цикл, обрабатывающий задачи для каждого этапа этапа
func (o *Orchestrator) runStageWorker(ctx context.Context, stage config.StageConfig) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// ============================================= ЗАХВАТ ЗАДАЧИ И ПРОВЕРКИ ===================================================

		// 1. Проверяем, есть ли хоть одна задача для этого этапа
		has, err := o.tasksRepo.HasPendingTasks(ctx, stage.StatusPending)
		if err != nil { // какая-то ошибка при проверки задач
			slog.Error(fmt.Sprintf("Error checking pending tasks for %s:", stage.Name), "err", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if !has { // если пока нет, ждем несколько секунд
			time.Sleep(2 * time.Second)
			continue
		}

		// 2. Захват ресурсов с учетом возможного отзыва контекста
		if err := o.resourceManager.AcquireWithContext(ctx, stage.Quota); err != nil {
			return // контекст завершён
		}

		// 3. Атомарно забираем задачу и обновляем ее статус (транзакция внутри репозитория)
		task, err := o.tasksRepo.ClaimNextTask(ctx, stage.StatusPending, stage.StatusProcessing)
		if err != nil {
			// может вернуть ошибку, если уже успели занять задачу и новых нет, либо внутренняя ошибка репозитория
			// в любом случае освобождаем память
			o.resourceManager.Release(stage.Quota)

			slog.Error(fmt.Sprintf("Error claiming task for %s:", stage.Name), "err", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// 4. мы уже взяли задачу, ошибки не произошло, поэтому освобождаем память в конце обработки
		// помещаем весь код в анонимную функцию, чтобы после conntinue выполнился defer с освобождением памяти
		func() {
			defer o.resourceManager.Release(stage.Quota)

			// ============================================= ГЕНЕРАЦИЯ URL ДЛЯ ВЗАИМОДЕЙСТВИЯ ===================================================
			inputKey := stage.InputKeyFunc(task.GroupID, task.TaskID)
			outputKey := stage.OutputKeyFunc(task.GroupID, task.TaskID)

			// генерируем ссылки длительностью максимального timeout, чтобы успели воркеры записать и ссылки не закончили действие
			inputURL, err := o.minio.GetPresignedGetLocalURL(ctx, inputKey, o.cfg.AIWorkersTimeoutHour)
			if err != nil {
				// ставим у задачи статус ошибки и переходим на следующую итерацию цикла
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("generate get URL s3 task for %s:", stage.Name), "err", err)
				return
			}
			outputURL, err := o.minio.GetPresignedPutURL(ctx, outputKey, o.cfg.AIWorkersTimeoutHour)
			if err != nil {
				// ставим у задачи статус ошибки и переходим на следующую итерацию цикла
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("generate put URL s3 task for %s:", stage.Name), "err", err)
				return
			}

			// ==================================== ОТПРАВКА ЗАПРОСА НА ОБРАБОТКУ И ПОЛУЧЕНИЕ ОТВЕТА ============================================
			// 1. собираем запрос исходя из модели
			type RequestDTO struct {
				InputURL   string `json:"input_url"`
				OutputURL  string `json:"output_url"`
				Prompt     string `json:"prompt,omitempty"`
				DenoiseURL string `json:"denoised_url,omitempty"`
			}

			var newRequest RequestDTO

			// если транскрибация, то передаем еще и ссылку на исходный файл помимо диаризации
			if stage.StatusProcessing == string(domains.StatusProcessingTranscribe) {
				// генерируем ссылку длительностью максимального timeout, чтобы успели воркеры записать и ссылки не закончили действие
				// на деноизинг аудио, чттобы транскрибация нормально работала
				denoiseURL, err := o.minio.GetPresignedGetLocalURL(ctx, config.DenoisedKey(task.GroupID, task.TaskID), o.cfg.AIWorkersTimeoutHour)
				if err != nil {
					// ставим у задачи статус ошибки и переходим на следующую итерацию цикла
					if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
						slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
					}
					slog.Error(fmt.Sprintf("generate get URL s3 task for %s:", stage.Name), "err", err)
					return
				}

				newRequest = RequestDTO{
					InputURL:   inputURL, // ссылка на диаризацию
					DenoiseURL: denoiseURL, // на аудио
					OutputURL:  outputURL, 
				}
				// если суммаризация, то передаем еще промпт запроса
			} else if stage.StatusProcessing == string(domains.StatusProcessingSummarize) {
				// получаем основной и дополнительный промпт по ID задачи
				promptsInfo, err := o.tasksRepo.SelectPromptsByTaskID(ctx, task.TaskID)
				if err != nil {
					// ставим у задачи статус ошибки и переходим на следующую итерацию цикла
					if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
						slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
					}
					slog.Error(fmt.Sprintf("generate put URL s3 task for %s:", stage.Name), "err", err)
					return
				}
				// собираем из них один нормально оформленный промпт
				fullPromptString := tools.BuildFullPrompt(promptsInfo)

				// получаем промпт дополнительный и основной, соединяем в один
				newRequest = RequestDTO{
					InputURL:  inputURL, // на транскрибацию
					Prompt:    fullPromptString, // на созданный промпт
					OutputURL: outputURL,
				}
			} else {
				// иначе базовый запрос
				newRequest = RequestDTO{
					InputURL:  inputURL,
					OutputURL: outputURL,
				}
			}

			// 2. сериализация json
			jsonData, err := json.Marshal(newRequest)
			if err != nil {
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("json marshal task for %s:", stage.Name), "err", err)
				return
			}

			// 3. создание запроса
			req, err := http.NewRequest("POST", stage.EndPoint, bytes.NewBuffer(jsonData))
			if err != nil {
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("create request for %s:", stage.Name), "err", err)
				return
			}
			req.Header.Set("Content-Type", "application/json")

			// 4. отправка запроса
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("send request for %s:", stage.Name), "err", err)
				return
			}
			defer resp.Body.Close() // заранее закрываем соединение

			// 5. обработка ответа
			if resp.StatusCode == 200 {
				// После конца обработки, если все ок, обновляем статус задачи
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.NextStatus); err != nil {
					slog.Error(fmt.Sprintf("failed change new status after the end of processing %s:", stage.Name), "err", err)
					return
				}
			} else {
				// иначе ставим ошибку в статус задачи
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("bad response for %s:", stage.Name), "err", err)
				return
			}

			// =============================== ЕСЛИ НОВЫЙ СТАТУС DONE, СБОРКА RESULT_JSON И ДОБАВЛЕНИЕ В БД =======================================
			if stage.NextStatus == string(domains.StatusDone) {
				// 1. генерируем ссылку на диаризацию по ключу input
				diarizeURL, err := o.minio.GetPresignedGetPublicURL(ctx, inputKey, o.cfg.AIWorkersTimeoutHour, o.cfg.PresignedPublicHostName)
				if err != nil {
					// ставим у задачи статус ошибки и переходим на следующую итерацию цикла
					if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
						slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
					}
					slog.Error(fmt.Sprintf("generate get URL s3 task for %s:", stage.Name), "err", err)
					return
				}
				// 2. генерируем ссылку на саммари по ключу output
				summaryURL, err := o.minio.GetPresignedGetPublicURL(ctx, outputKey, o.cfg.AIWorkersTimeoutHour, o.cfg.PresignedPublicHostName)
				if err != nil {
					// ставим у задачи статус ошибки и переходим на следующую итерацию цикла
					if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
						slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
					}
					slog.Error(fmt.Sprintf("generate put URL s3 task for %s:", stage.Name), "err", err)
					return
				}

				// 3. собираем ответ для результатов
				resultData := map[string]string{
					"diarize": diarizeURL,
					"summary": summaryURL,
				}

				// 4. сериализация json
				resultJSON, err := json.Marshal(resultData)
				if err != nil {
					// ставим статус ошибки
					if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
						slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
					}
					slog.Error(fmt.Sprintf("json marshal result for %s:", stage.Name), "err", err)
					return
				}

				// 5. Обновляем result_json
				if err := o.tasksRepo.UpdateTaskResult(ctx, task.TaskID, resultJSON); err != nil {
					slog.Error(fmt.Sprintf("failed save result for %s:", stage.Name), "err", err)
					return
				}
			}
		}()
	}
}
