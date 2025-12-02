#!/bin/bash
set -e

# Navigate to the directory where the script is located
cd "$(dirname "$0")"

echo "Stopping containers and removing volumes..."
docker-compose down -v

echo "Cleaning WAL archive..."
# Remove contents of Wal_Archive but keep the directory
if [ -d "Wal_Archive" ]; then
    rm -rf Wal_Archive/*
fi

echo "Starting all containers (Primary, Standby, pgAdmin, restore_target, wal_capturer)..."
docker-compose up -d --build pg_primary pg_standby pgadmin restore_target wal_capturer

echo "Done. Servers are recreated and pgAdmin should show them."
