CREATE TABLE tasks (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_name VARCHAR(255) NOT NULL,
    description TEXT,
    meeting_date DATE ,
    asr_model VARCHAR(50) NOT NULL,
    llm_model VARCHAR(50) NOT NULL,
    tokens INTEGER NOT NULL,
    summary_prompt TEXT NOT NULL,
    additional_prompt TEXT,
    file_name VARCHAR(512) NOT NULL,
    duration INTEGER DEFAULT 0,
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    result_json JSONB,
    stage_entered_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
    started_at TIMESTAMP,
    competed_at TIMESTAMP
);

CREATE INDEX idx_tasks_user_id ON tasks(user_id);
CREATE INDEX idx_tasks_status ON tasks(status);