package service

import (
	"accelerator/internal/core/config"
	"accelerator/internal/features/tasks/repository"
)

type TasksService struct {
	repo *repository.TasksRepo
	cfg  *config.Config
}

func NewTasksService(repo *repository.TasksRepo, cfg *config.Config) *TasksService {
	return &TasksService{
		repo: repo,
		cfg:  cfg,
	}
}
