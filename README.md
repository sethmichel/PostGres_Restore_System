milestones
1) Understanding. Build the mental model and infrastructure that everything else depends on.
	- Physical vs logical replication
	- Go vs python
	- What a timeline is
	- How a base back up works using pg_basebackup or copying the raw data directory while the server is in backup mode
	- Where checkpoints happen, and how pg expects to replay WAL.

2) Continuous wal capture + local storage format. Build the piece that acts like RDS/Azure’s “WAL Archive Service.”
	- Connect to the primary replication slot, get all the wal changes, store them locally
	- Add a metadata table and update it for each wal chunk
        § snapshot_id
        § snapshot_lsn_start
        § snapshot_lsn_end (when next snapshot begins)
        § timeline
        § WAL files needed
        § Expiration/retention flags
    - Later
		§ Wal compression, deduplication, use cloud instead of local

3) Snapshot manager + metadata tracking. Build the system that takes base snapshots and keeps track of what WAL belongs to which snapshot.
	- Make a snapshot scheduler: take a snapshot every 6 hours or something
	- Make a snapshot manifest that has, the snapshot id, timestamp, bas lsn, path, wal range required to recover from this snapshot

4) Restore Engine + Point-in-Time Replay. Build the code that reconstructs the database to any chosen point.
	- Choose a target timestamp or target LSN.
	- Query metadata to find:
		• the correct snapshot
		• which WAL chunks cover the target
	- Create a restoration directory (fresh Postgres data folder).
	- Copy snapshot files into it.
	- Configure recovery.conf or Postgres 16+ equivalent:
		• set restore_command to fetch WAL from your archive
		• set recovery_target_time or recovery_target_lsn
	- Launch Postgres in restore mode.
	- It replays your archived WAL until the target moment.

5) Automation, Policies, CLI, and Cloud Features. Turn your system into a polished resume-grade product
	- Snapshot rotation policies:
		• e.g. retain last 7 dailies + last 4 weeklies
	- WAL retention policies
	- Optional encryption for cloud storage
	- CLI tool with commands like:
		• pitr snapshot create
		• pitr wal sync
		• pitr restore --to "2025-11-23 14:41:37"
	- Status dashboard (minimal):
		• Last snapshot timestamp
		• Last WAL received LSN
        • Archive size, retention health



Pg_basebackup
Pg_recvlogical






run docker setup
docker compose down -v # nukes it

docker compose build --no-cache

docker compose up -d


