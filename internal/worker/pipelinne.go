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
	"accelerator/internal/core/error_type"
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
		fmt.Printf("Orchestrator: starting worker for stage=%s\n", st.Name)
		go o.runStageWorker(ctx, st) // запускаем вечный цикл для каждой
	}
}

// вечный цикл, обрабатывающий задачи для каждого этапа этапа
func (o *Orchestrator) runStageWorker(ctx context.Context, stage config.StageConfig) {
	defer func() {
        if r := recover(); r != nil {
			fmt.Printf("runStageWorker: stage=%s, PANIC!!!!!!!!!!!!!!!!\n", stage.Name)
            slog.Error("panic in runStageWorker", "stage", stage.Name, "panic", r)
        }
    }()


	for {
		select {
		case <-ctx.Done():
			fmt.Printf("runStageWorker: stage=%s, context done, exiting\n", stage.Name)
			return
		default:
		}

		// ============================================= ЗАХВАТ ЗАДАЧИ И ПРОВЕРКИ ===================================================

		// 1. Атомарно забираем задачу и обновляем ее статус (транзакция внутри репозитория)
		task, err := o.tasksRepo.ClaimNextTask(ctx, stage.StatusPending, stage.StatusProcessing)
		if err != nil {
			// может вернуть ошибку, если уже успели занять задачу и новых нет, либо внутренняя ошибка репозитория
			if error_type.IsNotFound(err) {
				// Это не ошибка, просто ждём
				fmt.Printf("runStageWorker: stage=%s, no task claimed (not found), sleeping 2s\n", stage.Name)
				time.Sleep(2 * time.Second)
			} else {
				slog.Error(fmt.Sprintf("Error claiming task for %s:", stage.Name), "err", err)
				fmt.Printf("runStageWorker: stage=%s, error claiming task: %v\n", stage.Name, err)
				time.Sleep(1 * time.Second)
			}
			continue
		}
		fmt.Printf("runStageWorker: stage=%s, claimed task id=%s, groupID=%s\n", stage.Name, task.TaskID, task.GroupID)

		// 2. Захват ресурсов с учетом возможного отзыва контекста
		if err := o.resourceManager.AcquireWithContext(ctx, stage.Quota); err != nil {
			fmt.Printf("runStageWorker: stage=%s, failed to acquire resources (context done): %v\n", stage.Name, err)
			return // контекст завершён
		}
		fmt.Printf("runStageWorker: stage=%s, acquired quota=%d\n", stage.Name, stage.Quota)

		// 4. мы уже взяли задачу, ошибки не произошло, поэтому освобождаем память в конце обработки
		// помещаем весь код в анонимную функцию, чтобы после conntinue выполнился defer с освобождением памяти
		func() {
			defer o.resourceManager.Release(stage.Quota)
			fmt.Printf("runStageWorker: stage=%s, processing task id=%s\n", stage.Name, task.TaskID)

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
				fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to generate inputURL: %v\n", stage.Name, task.TaskID, err)
				return
			}
			outputURL, err := o.minio.GetPresignedPutURL(ctx, outputKey, o.cfg.AIWorkersTimeoutHour)
			if err != nil {
				// ставим у задачи статус ошибки и переходим на следующую итерацию цикла
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("generate put URL s3 task for %s:", stage.Name), "err", err)
				fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to generate outputURL: %v\n", stage.Name, task.TaskID, err)
				return
			}
			fmt.Printf("runStageWorker: stage=%s, task id=%s, generated URLs (input, output)\n", stage.Name, task.TaskID)

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
					fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to generate denoiseURL: %v\n", stage.Name, task.TaskID, err)
					return
				}

				newRequest = RequestDTO{
					InputURL:   inputURL, // ссылка на диаризацию
					DenoiseURL: denoiseURL, // на аудио
					OutputURL:  outputURL, 
				}
				fmt.Printf("runStageWorker: stage=%s, task id=%s, built transcribe request with denoiseURL\n", stage.Name, task.TaskID)
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
					fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to get prompts: %v\n", stage.Name, task.TaskID, err)
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
				fmt.Printf("runStageWorker: stage=%s, task id=%s, built summarize request with prompt\n", stage.Name, task.TaskID)
			} else {
				// иначе базовый запрос
				newRequest = RequestDTO{
					InputURL:  inputURL,
					OutputURL: outputURL,
				}
				fmt.Printf("runStageWorker: stage=%s, task id=%s, built basic request\n", stage.Name, task.TaskID)
			}

			// 2. сериализация json
			jsonData, err := json.Marshal(newRequest)
			if err != nil {
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("json marshal task for %s:", stage.Name), "err", err)
				fmt.Printf("runStageWorker: stage=%s, task id=%s, json marshal error: %v\n", stage.Name, task.TaskID, err)
				return
			}

			// 3. создание запроса
			req, err := http.NewRequest("POST", stage.EndPoint, bytes.NewBuffer(jsonData))
			if err != nil {
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("create request for %s:", stage.Name), "err", err)
				fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to create HTTP request: %v\n", stage.Name, task.TaskID, err)
				return
			}
			req.Header.Set("Content-Type", "application/json")

			// 4. отправка запроса
			client := &http.Client{}
			fmt.Printf("runStageWorker: stage=%s, task id=%s, sending request to %s\n", stage.Name, task.TaskID, stage.EndPoint)
			resp, err := client.Do(req)
			if err != nil {
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("send request for %s:", stage.Name), "err", err)
				fmt.Printf("runStageWorker: stage=%s, task id=%s, request failed: %v\n", stage.Name, task.TaskID, err)
				return
			}
			defer resp.Body.Close() // заранее закрываем соединение
			fmt.Printf("runStageWorker: stage=%s, task id=%s, received response status %d\n", stage.Name, task.TaskID, resp.StatusCode)

			// 5. обработка ответа
			if resp.StatusCode == 200 {
				// После конца обработки, если все ок, обновляем статус задачи
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.NextStatus); err != nil {
					slog.Error(fmt.Sprintf("failed change new status after the end of processing %s:", stage.Name), "err", err)
					fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to update status to %s: %v\n", stage.Name, task.TaskID, stage.NextStatus, err)
					return
				}
				fmt.Printf("runStageWorker: stage=%s, task id=%s, status updated to %s\n", stage.Name, task.TaskID, stage.NextStatus)
			} else {
				// иначе ставим ошибку в статус задачи
				if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
					slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
				}
				slog.Error(fmt.Sprintf("bad response for %s:", stage.Name), "err", err)
				fmt.Printf("runStageWorker: stage=%s, task id=%s, bad response status %d, setting error status\n", stage.Name, task.TaskID, resp.StatusCode)
				return
			}

			// =============================== ЕСЛИ НОВЫЙ СТАТУС DONE, СБОРКА RESULT_JSON И ДОБАВЛЕНИЕ В БД =======================================
			if stage.NextStatus == string(domains.StatusDone) {
				fmt.Printf("runStageWorker: stage=%s, task id=%s, finalizing result JSON\n", stage.Name, task.TaskID)
				// 1. генерируем ссылку на диаризацию по ключу input
				diarizeURL, err := o.minio.GetPresignedGetPublicURL(ctx, inputKey, o.cfg.AIWorkersTimeoutHour, o.cfg.PresignedPublicHostName)
				if err != nil {
					// ставим у задачи статус ошибки и переходим на следующую итерацию цикла
					if err := o.tasksRepo.UpdateTaskStatus(ctx, task.TaskID, stage.StatusError); err != nil {
						slog.Error(fmt.Sprintf("failed change status error %s:", stage.Name), "err", err)
					}
					slog.Error(fmt.Sprintf("generate get URL s3 task for %s:", stage.Name), "err", err)
					fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to generate diarize public URL: %v\n", stage.Name, task.TaskID, err)
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
					fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to generate summary public URL: %v\n", stage.Name, task.TaskID, err)
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
					fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to marshal result JSON: %v\n", stage.Name, task.TaskID, err)
					return
				}

				// 5. Обновляем result_json
				if err := o.tasksRepo.UpdateTaskResult(ctx, task.TaskID, resultJSON); err != nil {
					slog.Error(fmt.Sprintf("failed save result for %s:", stage.Name), "err", err)
					fmt.Printf("runStageWorker: stage=%s, task id=%s, failed to save result JSON: %v\n", stage.Name, task.TaskID, err)
					return
				}
				fmt.Printf("runStageWorker: stage=%s, task id=%s, result JSON saved successfully\n", stage.Name, task.TaskID)
			}
			fmt.Printf("runStageWorker: stage=%s, task id=%s, processing finished successfully\n", stage.Name, task.TaskID)
		}()
	}
}
