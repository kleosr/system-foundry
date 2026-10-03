#!/bin/sh
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -v app_password="$FOUNDRY_APP_PASSWORD" <<'SQL'
ALTER ROLE foundry_app LOGIN PASSWORD :'app_password';
SQL
