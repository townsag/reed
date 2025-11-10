#!/bin/bash
set -e

echo "Initializing user_service database ..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname user_service -f /app-sql/user_service_init.sql

echo "Initializing document_service database ..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname document_service -f /app-sql/document_service_init.sql

echo "All databases initialized successfully!"