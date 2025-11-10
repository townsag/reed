#!/bin/bash
set -e

echo "Creating databases..."
# this does not need a password because the script runs inside of the docker container
# as part of the database initialization process
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
    CREATE DATABASE user_service;
    CREATE DATABASE document_service;
EOSQL

echo "Databases created successfully!"