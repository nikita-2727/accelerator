package repository

import (
	"accelerator/internal/core/error_type"
	"accelerator/internal/domains"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TasksRepo struct {
	pool *pgxpool.Pool
}

func NewTasksRepo(pool *pgxpool.Pool) *TasksRepo {
	return &TasksRepo{
		pool: pool,
	}
}

type executor interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (repo *TasksRepo) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return repo.pool.BeginTx(ctx, opts)
}

// ======================================= ДОПОЛНИТЕЛЬНЫЕ ФУНКЦИИ ===================================

// ищет пользователя по ID
// возвращает информацию о нем
func (repo *TasksRepo) SelectUserByID(ctx context.Context, userID string) (*domains.User, error) {
	sqlQuery := `
	SELECT id, login, full_name, position, role, created_at
	FROM users
	where id = $1;
	`

	var userInfo domains.User
	err := repo.pool.QueryRow(ctx, sqlQuery, userID).Scan(
		&userInfo.ID,
		&userInfo.Login,
		&userInfo.FullName,
		&userInfo.Position,
		&userInfo.Role,
		&userInfo.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) { // специальный тип ошибки, если ничего не вернулось
		return nil, error_type.NewNotFound("Пользователь, над которым хотят совершить действие не найден")
	} else if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("get user info: %w", err))
	}

	return &userInfo, nil
}

// проверяет членство в группе = true - есть в группе
func (repo *TasksRepo) IsUserIntoGroup(ctx context.Context, userID, groupID string) (bool, error) {
	query := `SELECT EXISTS (SELECT 1 FROM group_members WHERE user_id = $1 AND group_id = $2)`
	var exists bool
	err := repo.pool.QueryRow(ctx, query, userID, groupID).Scan(&exists)
	if err != nil {
		return false, error_type.NewInternal(fmt.Errorf("check membership: %w", err))
	}
	return exists, nil
}

// проверяет существование группы = true - группа существует
func (repo *TasksRepo) CheckGroup(ctx context.Context, groupID string) (bool, error) {
	query := `SELECT EXISTS (SELECT 1 FROM groups WHERE id = $1)`
	var check bool
	err := repo.pool.QueryRow(ctx, query, groupID).Scan(&check)
	if err != nil {
		return false, error_type.NewInternal(fmt.Errorf("check group: %w", err))
	}
	return check, nil
}

// ================================== ИЗМЕНЕНИЕ СТАТУСА ЗАДАЧИ ===================================

func (repo *TasksRepo) UpdateTaskStatus(ctx context.Context, taskID, statusTask string) error {
	sqlQuery := `
		UPDATE tasks SET status = $1, stage_entered_at = NOW(), updated_at = NOW()
		WHERE id = $2;
	`
	result, err := repo.pool.Exec(ctx, sqlQuery, statusTask, taskID)
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("update status task: %w", err))
	}

	if !result.Update() { // возвращает false, если строка не была обновлена
		return error_type.NewNotFound("Задача не найдена")
	}

	return nil
}

func (repo *TasksRepo) updateTaskSuccessUpload(ctx context.Context, e executor, taskID, filePath string, duration int, newStatus string) error {
	sqlQuery := `
		UPDATE tasks SET file_path = $1, duration = $2, status = $3, stage_entered_at = NOW(), updated_at = NOW()
		WHERE id = $4;
	`
	result, err := e.Exec(ctx, sqlQuery, filePath, duration, newStatus, taskID)
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("update status task: %w", err))
	}

	if !result.Update() { // возвращает false, если строка не была обновлена
		return error_type.NewNotFound("Задача не найдена")
	}

	return nil
}

func (repo *TasksRepo) UpdateTaskSuccessUpload(ctx context.Context, taskID, filePath string, duration int, newStatus string) error {
	return repo.updateTaskSuccessUpload(ctx, repo.pool, taskID, filePath, duration, newStatus)
}
func (repo *TasksRepo) UpdateTaskSuccessUploadTx(ctx context.Context, e executor, taskID, filePath string, duration int, newStatus string) error {
	return repo.updateTaskSuccessUpload(ctx, e, taskID, filePath, duration, newStatus)
}

// обновляет result_json и завершает задачу (completed_at)
// использовать только при status == done
func (r *TasksRepo) UpdateTaskResult(ctx context.Context, taskID string, resultJSON []byte) error {
	query := `
        UPDATE tasks
        SET result_json = $1,
            completed_at = NOW(),
            updated_at = NOW()
        WHERE id = $2;
    `
	_, err := r.pool.Exec(ctx, query, resultJSON, taskID)
	if err != nil {
		return fmt.Errorf("update task status and result: %w", err)
	}
	return nil
}

// ================================== ПОЛУЧЕНИЕ СТАТУСА ЗАДАЧИ ===================================

func (repo *TasksRepo) SelectTaskStatus(ctx context.Context, taskID string) (string, error) {
	sqlQuery := `
		SELECT status
		FROM tasks
		WHERE id = $1;
	`

	var status string

	err := repo.pool.QueryRow(ctx, sqlQuery, taskID).Scan(&status)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", error_type.NewNotFound("Задача не найдена")
	} else if err != nil {
		return "", error_type.NewInternal(fmt.Errorf("select status task: %w", err))
	}

	return status, nil
}

// ================================ РАБОТА С ОЧЕРЕДЬЮ ЗАДАЧ ======================================

// возвращает, сколько задач выше по приориттету при текущем статусе задачи с ID = taskID
func (repo *TasksRepo) GetQueuePosition(ctx context.Context, status, taskID string) (int, error) {
	query := `
        WITH target AS (
            SELECT created_at, stage_entered_at, duration
            FROM tasks
            WHERE id = $2
        )
        SELECT COUNT(*)
        FROM tasks t, target
        WHERE t.status = $1
    	  AND t.id != $2
          AND (
              (EXTRACT(EPOCH FROM (NOW() - t.created_at)) * 0.2
               + EXTRACT(EPOCH FROM (NOW() - t.stage_entered_at)) * 1.0
              ) / NULLIF(t.duration, 1)
          ) > (
              (EXTRACT(EPOCH FROM (NOW() - target.created_at)) * 0.2
               + EXTRACT(EPOCH FROM (NOW() - target.stage_entered_at)) * 1.0
              ) / NULLIF(target.duration, 1)
          );
    `
	var position int
	err := repo.pool.QueryRow(ctx, query, status, taskID).Scan(&position)
	if err != nil {
		return 0, error_type.NewInternal(fmt.Errorf("get queue position: %w", err))
	}
	return position, nil
}

// транзакционная функция, сначала получает информация о задаче, потом изменяет ее статус на процессинг
func (r *TasksRepo) ClaimNextTask(ctx context.Context, statusPending, statusProcessing string) (*domains.Task, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	query := `
        SELECT id, user_id, group_id, task_name, description, meeting_date::text,
               pattern_id, file_path, file_name, duration, status, result_json,
               created_at, updated_at, started_at, completed_at
        FROM tasks
        WHERE status = $1
        ORDER BY ((EXTRACT(EPOCH FROM (NOW() - created_at)) * 0.2 + EXTRACT(EPOCH FROM (NOW() - stage_entered_at)) * 1.0) / NULLIF(duration, 1)) DESC
        LIMIT 1
        FOR UPDATE SKIP LOCKED
    `

	var (
		task        domains.Task
		filePath    *string
		duration    *int
		startedAt   *time.Time
		completedAt *time.Time
	)

	err = tx.QueryRow(ctx, query, statusPending).Scan(
		&task.TaskID, &task.UserID, &task.GroupID, &task.TaskName, &task.Description, &task.MeetingDate,
		&task.PatternID, &filePath, &task.FileName, &duration, &task.Status, &task.ResultJson,
		&task.CreatedAt, &task.UpdatedAt, &startedAt, &completedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, error_type.NewNotFound("no pending tasks")
		}
		fmt.Println("ClaimNextTask SQL error !!!!!!!!!!!!!!!!!!!", "err", err)
		return nil, error_type.NewInternal(fmt.Errorf("select next tasks in queue: %w", err))
	}

	// Обработка nullable полей
	if filePath != nil {
		task.FilePath = *filePath
	}
	if duration != nil {
		task.Duration = *duration
	}
	if startedAt != nil {
		task.StartedAt = *startedAt
	}
	if completedAt != nil {
		task.CompletedAt = *completedAt
	}

	// обновляем статус задачи, чтобы другая горутина уже не могла ее взять
	_, err = tx.Exec(ctx, `UPDATE tasks SET status = $1, started_at = NOW(), stage_entered_at = NOW() WHERE id = $2;`, statusProcessing, task.TaskID)
	if err != nil {
		fmt.Println("ClaimNextTask SQL error !!!!!!!!!", "err", err)
		return nil, error_type.NewInternal(fmt.Errorf("update next tasks in queue: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	task.Status = statusProcessing

	return &task, nil
}

// ==================================== СОЗДАНИЕ ЗАДАЧИ ==========================================

func (repo *TasksRepo) CreateTask(
	ctx context.Context,
	taskID, userID, groupID,
	taskName, taskDescription, meetingDate, patternID,
	fileName, filePath, statusTask string,
) (*domains.Task, error) {
	sqlQuery := `
	INSERT INTO tasks (id, user_id, group_id, task_name, description, meeting_date, pattern_id, file_path, file_name, status)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	RETURNING id, group_id, task_name, description, meeting_date::text, pattern_id, file_name, status, created_at
	`
	var task domains.Task

	err := repo.pool.QueryRow(
		ctx, sqlQuery,
		taskID, userID, groupID, taskName, taskDescription, meetingDate, patternID, filePath, fileName, statusTask,
	).Scan(
		&task.TaskID,
		&task.GroupID,
		&task.TaskName,
		&task.Description,
		&task.MeetingDate,
		&task.PatternID,
		&task.FileName,
		&task.Status,
		&task.CreatedAt,
	)

	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("create task: %w", err))
	}

	return &task, nil
}

// =================================== ПОЛУЧЕНИЕ ЗАДАЧИ ПО ID ==============================================

// возвращает задачу по её ID
// Если задача не найдена, возвращает error_type.NewNotFound
func (repo *TasksRepo) SelectTaskByID(ctx context.Context, taskID string) (*domains.Task, error) {
	query := `
        SELECT 
            id, user_id, group_id, task_name, description, meeting_date::text,
            pattern_id, file_path, file_name, duration, status, result_json,
            stage_entered_at, created_at, updated_at, started_at, completed_at
        FROM tasks
        WHERE id = $1
    `

	var (
		task        domains.Task
		filePath    *string
		duration    *int
		startedAt   *time.Time
		completedAt *time.Time
	)

	err := repo.pool.QueryRow(ctx, query, taskID).Scan(
		&task.TaskID,
		&task.UserID,
		&task.GroupID,
		&task.TaskName,
		&task.Description,
		&task.MeetingDate,
		&task.PatternID,
		&filePath,
		&task.FileName,
		&duration,
		&task.Status,
		&task.ResultJson,
		&task.StageEnteredAt,
		&task.CreatedAt,
		&task.UpdatedAt,
		&startedAt,
		&completedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, error_type.NewNotFound("задача не найдена")
		}
		return nil, error_type.NewInternal(fmt.Errorf("find task by id: %w", err))
	}

	// Обработка nullable полей
	if filePath != nil {
		task.FilePath = *filePath
	}
	if duration != nil {
		task.Duration = *duration
	}
	if startedAt != nil {
		task.StartedAt = *startedAt
	}
	if completedAt != nil {
		task.CompletedAt = *completedAt
	}

	return &task, nil
}

// ============================== ПОЛУЧЕНИЕ ВСЕХ ЗАДАЧ ГРУППЫ ==============================================
// возвращает список задач группы и общее количество (для пагинации)
// Если limit <= 0, пагинация игнорируется и total равен количеству записей в ответе
func (repo *TasksRepo) GetTasksByGroupID(ctx context.Context, groupID string, page, limit int) (*[]domains.Task, int64, error) {
	var tasks []domains.Task
	var total int64

	// Базовый запрос без пагинации
	baseQuery := `
        SELECT id, user_id, group_id, task_name, description, meeting_date::text,
               pattern_id, file_path, file_name, duration, status, result_json,
               stage_entered_at, created_at, updated_at, started_at, completed_at
        FROM tasks
        WHERE group_id = $1
        ORDER BY created_at DESC`

	if limit > 0 {
		offset := (page - 1) * limit

		// Получаем общее количество
		countQuery := `SELECT COUNT(*) FROM tasks WHERE group_id = $1`
		if err := repo.pool.QueryRow(ctx, countQuery, groupID).Scan(&total); err != nil {
			return nil, 0, error_type.NewInternal(fmt.Errorf("count tasks: %w", err))
		}

		// Основной запрос с лимитом
		query := baseQuery + ` LIMIT $2 OFFSET $3`
		rows, err := repo.pool.Query(ctx, query, groupID, limit, offset)
		if err != nil {
			return nil, 0, error_type.NewInternal(fmt.Errorf("select tasks: %w", err))
		}
		defer rows.Close()

		for rows.Next() {
			// обновляем все значения по умолчанию
			var (
				task        domains.Task
				filePath    *string
				duration    *int
				startedAt   *time.Time
				completedAt *time.Time
			)

			if err := rows.Scan(
				&task.TaskID, &task.UserID, &task.GroupID, &task.TaskName, &task.Description, &task.MeetingDate,
				&task.PatternID, &filePath, &task.FileName, &duration, &task.Status, &task.ResultJson,
				&task.StageEnteredAt, &task.CreatedAt, &task.UpdatedAt, &startedAt, &completedAt,
			); err != nil {
				return nil, 0, error_type.NewInternal(fmt.Errorf("scan task: %w", err))
			}

			// Обработка nullable полей
			if filePath != nil {
				task.FilePath = *filePath
			}
			if duration != nil {
				task.Duration = *duration
			}
			if startedAt != nil {
				task.StartedAt = *startedAt
			}
			if completedAt != nil {
				task.CompletedAt = *completedAt
			}

			tasks = append(tasks, task)
		}
	} else {
		// Без пагинации – возвращаем всё
		rows, err := repo.pool.Query(ctx, baseQuery, groupID)
		if err != nil {
			return nil, 0, error_type.NewInternal(fmt.Errorf("select all tasks: %w", err))
		}
		defer rows.Close()

		for rows.Next() {
			var (
				task        domains.Task
				filePath    *string
				duration    *int
				startedAt   *time.Time
				completedAt *time.Time
			)

			if err := rows.Scan(
				&task.TaskID, &task.UserID, &task.GroupID, &task.TaskName, &task.Description, &task.MeetingDate,
				&task.PatternID, &filePath, &task.FileName, &duration, &task.Status, &task.ResultJson,
				&task.StageEnteredAt, &task.CreatedAt, &task.UpdatedAt, &startedAt, &completedAt,
			); err != nil {
				return nil, 0, error_type.NewInternal(fmt.Errorf("scan task: %w", err))
			}

			// Обработка nullable полей
			if filePath != nil {
				task.FilePath = *filePath
			}
			if duration != nil {
				task.Duration = *duration
			}
			if startedAt != nil {
				task.StartedAt = *startedAt
			}
			if completedAt != nil {
				task.CompletedAt = *completedAt
			}

			tasks = append(tasks, task)
		}

		total = int64(len(tasks))
	}

	return &tasks, total, nil
}

// ======================================= ИЗМЕНЕНИЕ ЗАДАЧИ ==============================================

// динамическое обновление полей группы (точка входа для сервиса)
func (repo *TasksRepo) EditTask(ctx context.Context, taskID string, editInfo map[string]string) (*domains.Task, error) {
	// Фильтруем допустимые поля: task_name, description, meeting_date
	// валидируем в хэндлере, но пусть будет на всякий
	allowed := map[string]bool{"task_name": true, "description": true, "meeting_date": true}
	setClauses := make([]string, 0)
	args := make([]any, 0)
	i := 1
	for field, value := range editInfo {
		// проверяем на допустимость переданное поле
		if !allowed[field] {
			continue
		}

		args = append(args, value)

		// собираем массив для SET
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", field, i))
		i++
	}
	// также валидируем в хэндлере, но пусть будет
	// если где-то ошиблись с названиями
	if len(setClauses) == 0 {
		// Если нет допустимых полей, возвращаем текущую информацию о группе без изменений
		return repo.SelectTaskByID(ctx, taskID)
	}
	// добавляем в конец ID чтобы потом распаковать
	args = append(args, taskID)

	query := fmt.Sprintf(`
        UPDATE tasks SET %s, updated_at = NOW()
        WHERE id = $%d
        RETURNING id, user_id, group_id, task_name, description, meeting_date::text,
        	pattern_id, file_path, file_name, duration, status, result_json,
        	stage_entered_at, created_at, updated_at, started_at, completed_at;
    `, strings.Join(setClauses, ", "), i)

	var (
		task        domains.Task
		filePath    *string
		duration    *int
		startedAt   *time.Time
		completedAt *time.Time
	)

	err := repo.pool.QueryRow(ctx, query, args...).Scan(
		&task.TaskID,
		&task.UserID,
		&task.GroupID,
		&task.TaskName,
		&task.Description,
		&task.MeetingDate,
		&task.PatternID,
		&filePath,
		&task.FileName,
		&duration,
		&task.Status,
		&task.ResultJson,
		&task.StageEnteredAt,
		&task.CreatedAt,
		&task.UpdatedAt,
		&startedAt,
		&completedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, error_type.NewNotFound("Задача не найдена")
	} else if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("edit task: %w", err))
	}

	// Обработка nullable полей
	if filePath != nil {
		task.FilePath = *filePath
	}
	if duration != nil {
		task.Duration = *duration
	}
	if startedAt != nil {
		task.StartedAt = *startedAt
	}
	if completedAt != nil {
		task.CompletedAt = *completedAt
	}

	return &task, nil
}

// ======================================= УДАЛЕНИЕ ЗАДАЧИ ==============================================

func (repo *TasksRepo) DeleteTask(ctx context.Context, taskID string) error {
	sqlQuery := `
		DELETE FROM tasks
		WHERE id = $1;
	`

	result, err := repo.pool.Exec(ctx, sqlQuery, taskID)
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("delete task: %w", err))
	}

	rowsAffected := result.RowsAffected() // возвращает количество удаленных строк, если ноль
	if rowsAffected == 0 {                // то пользователь не найден
		return error_type.NewNotFound("Задача не найдена")
	}

	return nil
}

// ================================= ПОЛУЧЕНИЕ ПРОМПТОВ ПО ID ====================================

// SelectPromptsByTaskID возвращает prompt и additional_prompt из шаблона, привязанного к задаче.
// Если задача не найдена, возвращается error_type.NewNotFound.
// Если задача существует, но у неё нет назначенного шаблона, также возвращается error_type.NewNotFound.
func (repo *TasksRepo) SelectPromptsByTaskID(ctx context.Context, taskID string) (*domains.TaskPatternPrompts, error) {
	query := `
        SELECT p.summary_prompt, p.additional_prompt
        FROM tasks t
        JOIN patterns p ON t.pattern_id = p.id
        WHERE t.id = $1
    `

	var (
		prompts          domains.TaskPatternPrompts
		additionalPrompt []byte
	)

	err := repo.pool.QueryRow(ctx, query, taskID).Scan(
		&prompts.Prompt,
		&additionalPrompt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Уточняем причину: несуществующая задача или отсутствие шаблона
			var exists bool
			checkQuery := `SELECT EXISTS(SELECT 1 FROM tasks WHERE id = $1)`
			errCheck := repo.pool.QueryRow(ctx, checkQuery, taskID).Scan(&exists)
			if errCheck != nil {
				return nil, error_type.NewInternal(fmt.Errorf("проверка существования задачи: %w", errCheck))
			}
			if !exists {
				return nil, error_type.NewNotFound("задача не найдена")
			}
			return nil, error_type.NewNotFound("шаблон для задачи не назначен")
		}
		return nil, error_type.NewInternal(fmt.Errorf("получение промптов по задаче: %w", err))
	}

	if additionalPrompt != nil {
		prompts.AdditionalPrompt = json.RawMessage(additionalPrompt)
	}

	return &prompts, nil
}

// ======================================= ПРОВЕРКИ ==============================================

// проверяет, состоит ли пользователь в группе, к которой привязана задача
func (repo *TasksRepo) CheckUserInTaskGroup(ctx context.Context, userID, taskID string) (bool, error) {
	query := `
        SELECT EXISTS(
            SELECT 1
            FROM tasks t
            WHERE t.id = $1
              AND EXISTS (
                  SELECT 1
                  FROM group_members gm
                  WHERE gm.group_id = t.group_id
                    AND gm.user_id = $2
              )
        )
    `

	var exists bool
	err := repo.pool.QueryRow(ctx, query, taskID, userID).Scan(&exists)
	if err != nil {
		return false, error_type.NewInternal(fmt.Errorf("check user in task group: %w", err))
	}
	return exists, nil
}

// проверяет, есть ли хоть одна задача с нужным статусом, чттобы запустить воркер
func (repo *TasksRepo) HasPendingTasks(ctx context.Context, status string) (bool, error) {
	sqlQuery := `
		SELECT EXISTS (SELECT 1 FROM tasks WHERE status = $1 LIMIT 1)
	`

	var exists bool
	err := repo.pool.QueryRow(ctx, sqlQuery, status).Scan(&exists)
	if err != nil {
		return false, error_type.NewInternal(fmt.Errorf("has tasks with status: %w", err))
	}
	return exists, nil
}
