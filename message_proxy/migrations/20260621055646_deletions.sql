-- Add migration script here
CREATE TABLE IF NOT EXISTS deletions (
    topic_id UUID NOT NULL,
    user_id UUID NOT NULL,
    client_id BIGINT NOT NULL,
    delete_set int8multirange NOT NULL,
    PRIMARY KEY (topic_id, client_id)
)