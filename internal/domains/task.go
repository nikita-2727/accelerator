package domains

import "time"

type Task struct {
	TaskID string
	UserID string

	TaskName    string
	Description string
	MeetingDate time.Time

	ASRModel      string
	LLMModel      string
	Tokens        int
	SummaryPrompt string

	FilePath string
	FileName string
	Duration int

	Status string

 	ResultJson string

	CreatedAt   time.Time
	UpdatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
}
