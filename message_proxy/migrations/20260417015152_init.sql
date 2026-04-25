-- Add migration script here
CREATE TABLE IF NOT EXISTS messages (
    topic_id UUID NOT NULL,
    user_id UUID NOT NULL,
    message_offset INTEGER,
    content TEXT,
    PRIMARY KEY (topic_id, user_id, message_offset)
);

CREATE TABLE IF NOT EXISTS operations (
    topic_id UUID NOT NULL,
    user_id UUID NOT NULL,
    client_id BIGINT NOT NULL,  -- store a signed i64 here 
    operation_offset BIGINT NOT NULL,
    payload BYTEA NOT NULL,
    PRIMARY KEY (topic_id, client_id, operation_offset)
);