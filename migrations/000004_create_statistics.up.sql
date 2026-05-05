CREATE TABLE statistics (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    total_processed_minutes INTEGER NOT NULL DEFAULT 0,
    total_processed_files INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_statistics_user_id ON statistics(user_id);