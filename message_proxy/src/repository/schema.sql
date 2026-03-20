CREATE TABLE messages (
    topic_id UUID NOT NULL,
    user_id UUID NOT NULL,
    message_offset INTEGER,
    content TEXT,
    PRIMARY KEY (topic_id, user_id, message_offset)
);