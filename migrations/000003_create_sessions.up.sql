CREATE TABLE sessions (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,
    device_info TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
    expires_at INTEGER NOT NULL DEFAULT 600
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);