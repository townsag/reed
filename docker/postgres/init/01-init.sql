GRANT SELECT ON pg_stat_database TO admin;
GRANT pg_monitor TO admin;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;