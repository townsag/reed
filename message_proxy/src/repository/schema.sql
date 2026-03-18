CREATE TABLE messages (
    topic_id UUID PRIMARY KEY,
    user_id UUID PRIMARY KEY,
    offset INTEGER,
    content TEXT,
    PRIMARY KEY (topic_id, user_id, offset)
);