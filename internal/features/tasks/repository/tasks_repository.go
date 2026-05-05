package repository

import "github.com/jackc/pgx/v5/pgxpool"

type TasksRepo struct {
	pool *pgxpool.Pool
}

func NewTasksRepo(pool *pgxpool.Pool) *TasksRepo {
	return &TasksRepo{
		pool: pool,
	}
}
