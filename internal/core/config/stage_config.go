package config

import "accelerator/internal/domains"

// StageConfig описывает один этап конвейера.
type StageConfig struct {
	Name             string                              // "denoise", "transcribe", "diarize", "summarize"
	StatusPending    string                              // "pending_denoise"
	StatusProcessing string                              // "processing_denoise"
	NextStatus       string                              // "pending_transcribe" или "done"
	StatusError      string                              // "pending_error"
	EndPoint         string                              // адрес API для запуска скрипта
	Quota            ResourceQuota                       // квота ресурсов
	InputKeyFunc     func(groupID, taskID string) string // функция, которая будет возвращать нужный тип ключа, исходя из процесса
	OutputKeyFunc    func(groupID, taskID string) string // функция, которая будет генерировать ключ для выходных данных, исходя из процесса
}

func LoadStageConfig() *[]StageConfig {
	return &[]StageConfig{
		{
			Name:             "denoise",
			StatusPending:    string(domains.StatusPendingDenoise),
			StatusProcessing: string(domains.StatusProcessingDenoise),
			NextStatus:       string(domains.StatusPendingDiarize),
			StatusError:      string(domains.StatusErrorDenoise),
			EndPoint:         getEnvString("DENOISE_WORKER_URL", "denoise-worker/api/ai/denoise"),
			Quota:            StageQuotas[string(domains.StatusProcessingDenoise)],
			InputKeyFunc:     UploadKey,
			OutputKeyFunc:    DenoisedKey,
		},
		{
			Name:             "diarize",
			StatusPending:    string(domains.StatusPendingDiarize),
			StatusProcessing: string(domains.StatusProcessingDiarize),
			NextStatus:       string(domains.StatusPendingTranscribe),
			StatusError:      string(domains.StatusErrorDiarize),
			EndPoint:         getEnvString("DIARIZE_WORKER_URL", "diarize-worker/api/ai/diarize"),
			Quota:            StageQuotas[string(domains.StatusProcessingDiarize)],
			InputKeyFunc:     DenoisedKey,
			OutputKeyFunc:    DiarizationKey,
		},
		{
			Name:             "transcribe",
			StatusPending:    string(domains.StatusPendingTranscribe),
			StatusProcessing: string(domains.StatusProcessingTranscribe),
			NextStatus:       string(domains.StatusPendingSummarize),
			StatusError:      string(domains.StatusErrorTranscribe),
			EndPoint:         getEnvString("TRANSCRIBE_WORKER_URL", "asr-worker/api/ai/transcribe"),
			Quota:            StageQuotas[string(domains.StatusProcessingTranscribe)],
			InputKeyFunc:     DiarizationKey,
			OutputKeyFunc:    TranscriptKey,
		},
		{
			Name:             "summarize",
			StatusPending:    string(domains.StatusPendingSummarize),
			StatusProcessing: string(domains.StatusProcessingSummarize),
			NextStatus:       string(domains.StatusDone),
			StatusError:      string(domains.StatusErrorSummarize),
			EndPoint:         getEnvString("SUMMARY_WORKER_URL", "summarize-worker/api/ai/summarize"),
			Quota:            StageQuotas[string(domains.StatusProcessingSummarize)],
			InputKeyFunc:     TranscriptKey,
			OutputKeyFunc:    SummaryKey,
		},
	}
}

