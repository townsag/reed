CREATE TABLE users (
    -- id VARCHAR(32) PRIMARY KEY,
    id UUID PRIMARY KEY,
    user_name VARCHAR(32) NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    max_documents INTEGER DEFAULT 16,
    hashed_password VARCHAR(255) NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_modified TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_username ON users(user_name DESC);