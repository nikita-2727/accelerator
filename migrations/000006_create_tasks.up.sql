CREATE TABLE tasks (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    user_id UUID REFERENCES users(id), -- при удалении пользователя задача остается
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE, -- при удалении группы все задачи в группе удаляются
    task_name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    meeting_date DATE NOT NULL,
    pattern_id UUID REFERENCES patterns(id) NOT NULL, -- привязываем к шаблону промпта, которые создают админ или креатор
    file_path VARCHAR(512), -- загружается после в горутине, поэтому можетт быть NULL
    file_name VARCHAR(100) NOT NULL,
    duration INTEGER, -- загружается после в горутине, поэтому можетт быть NULL
    status VARCHAR(50) NOT NULL,
    result_json JSONB,
    stage_entered_at TIMESTAMP WITH TIME ZONE DEFAULT now(), -- последнее изменение статуса задачи для формулы приоритета
    created_at TIMESTAMP WITH TIME ZONE DEFAULT now(), -- время создания задачи
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT now(), -- фиксация любого изменения, служебное поле
    started_at TIMESTAMP, -- время, когда начался процесс обработки
    completed_at TIMESTAMP -- время окончания всех обработок, статус DONE
);

CREATE INDEX idx_tasks_group_id ON tasks(group_id);
CREATE INDEX idx_tasks_status ON tasks(status);