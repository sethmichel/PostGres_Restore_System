#!/bin/bash
set -e

# Configuration from environment variables
PG_HOST="${PG_HOST:-$HOST_NAME}"
PG_PORT="${PG_PORT:-${POSTGRES_PORT:-5432}}"
PG_USER="${PG_USER:-$POSTGRES_USER}"
PG_PASSWORD="${PG_PASSWORD:-$POSTGRES_PASSWORD}"
SLOT_NAME="${SLOT_NAME}"
ARCHIVE_DIR="${ARCHIVE_DIR:-/wal_archive}"

# Check for required variables
if [ -z "$PG_HOST" ] || [ -z "$PG_USER" ] || [ -z "$PG_PASSWORD" ] || [ -z "$SLOT_NAME" ]; then
  echo "Error: Missing required environment variables."
  echo "Required: PG_HOST (or HOST_NAME), PG_USER (or POSTGRES_USER), PG_PASSWORD (or POSTGRES_PASSWORD), SLOT_NAME"
  exit 1
fi

# Export password for pg_receivewal to use
export PGPASSWORD="${PG_PASSWORD}"

echo "Waiting for PostgreSQL primary to be ready..."
# Loop until we can connect to the primary
until pg_isready -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER"; do
  echo "Primary not ready, retrying in 2s..."
  sleep 2
done
echo "PostgreSQL primary is ready."

echo "Starting WAL capture to $ARCHIVE_DIR..."

# Run pg_receivewal
# --create-slot: Ensures the slot exists before starting
# --if-not-exists: Prevents error if slot already exists
# -v: Verbose output so you can see what it's doing in docker logs
exec pg_receivewal \
  -h "$PG_HOST" \
  -p "$PG_PORT" \
  -U "$PG_USER" \
  -D "$ARCHIVE_DIR" \
  --slot="$SLOT_NAME" \
  --create-slot \
  --if-not-exists \
  -v
