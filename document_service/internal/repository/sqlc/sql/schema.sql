-- partition the documents table on the document id
CREATE TABLE documents (
    id UUID PRIMARY KEY,
    name TEXT,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- the sort order makes a small difference if they are both in the same direction
-- because a forward index can be scanned backwards and vice versa. However
-- if one is ascending and the other is descending then the index might not
-- be used, need to verify this using query planner tool
CREATE INDEX idx_documents_created_at ON documents(created_at DESC, id DESC);
CREATE INDEX idx_documents_last_modified_at ON documents(last_modified_at DESC, id DESC);

-- guests should only be associated with one permission on one document
-- changes to the permission of a guest should be stored in either the permissions
-- table or the guests table?
-- there is a many to one relationship between the guests and documents table
-- this is captured in the guests table. Is this correct..?
-- partition the guests table on the document_id that guest is associated with
-- this guarantees that all the guests associated with a document are on the same
-- machine as the document record, preventing the need for multi postgres instance
-- joins
CREATE TABLE guests (
    id UUID PRIMARY KEY,
    -- add a foreign key to the guests table mapping to the documents table to
    -- enforce the constraint that each guest is associated with one and only
    -- one document. Keep this field in sync with the permissions table document_id
    -- field at the application level
    -- this may also make queries for all the links generated for a document easier
    document_id UUID NOT NULL REFERENCES documents(id),
    description TEXT,
    -- created by holds the user id that created this guest link
    -- only the creator of the link can modify it
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TYPE permission_level AS ENUM ('viewer', 'editor', 'owner');
CREATE TYPE recipient_type AS ENUM ('user', 'guest');

-- partition the permissions table on the document_id
-- this ensures that all the permissions on a document are on the same machine
-- still not sure what this means for queries that get permissions by user
CREATE TABLE permissions (
    recipient_id TEXT NOT NULL,
    recipient_type recipient_type NOT NULL,
    document_id UUID NOT NULL REFERENCES documents(id),
    permission_level permission_level NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (recipient_id, document_id)
);

-- this will be useful when we want to find all the editors/viewers on a document
CREATE INDEX idx_permissions_document ON permissions(document_id);

-- using the composite primary key of recipient_id and document_id means that we
-- will have a index on those two fields. 
-- TODO: Create an index on just the document_id
-- field of the permissions table to find all the recipients that a document has
-- been shared with

-- TODO: consider storing the document owner in the documents table...
-- rule, a document can only ever have onw owner, there should be a process for transferring ownership
-- of a document, ownership can still be stored in the permissions table. only the document owner can
-- add permissions to other users. Add user groups so that there is less burden on the owner of the
-- document to manage document permissions