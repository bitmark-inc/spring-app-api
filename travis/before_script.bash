
#!/usr/bin/env bash
set -eux

if [ "${PGVERSION-}" != "" ]
then
  psql -U postgres -c 'create database fbm'
  psql -U postgres -c "create user fbm SUPERUSER PASSWORD 'secret'"
  psql -U postgres -a -q -f ./store/postgres/pg_schema.sql
fi