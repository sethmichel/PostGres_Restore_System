

the things to check at start up
- primary and standby have a test data table
- I have my 2 physical replication slots on primary
- files/folders exist
- should have a shell script that will run when wal_catpure container runs. this will pull raw wal segments from the primary using pg_receivwal constantly
	**at this point, run_wal_captureer.sh will produce the raw wal segments
	**main.go and wal_capturer share access via a docker volume bind mount. because of this volume mapping, any file pg_receivwal writes to /wal_archive in the container instantly appears in docker_connection/wal_archieve. so open that dir in main.go to get the wal data
- main.go should open the wal_archieve files in a loop to get new info, now process that raw wal info
	**CHECKPOINT - see if main can access the info, see what it looks like, and see if the system is even getting the info

- the wal data is binary, but I don't convert them. I need them in binary format to do a restore. we instead will parse the filename to extract metadata. main.go should store this metadata in a table so the system knows what we have. so the main.go loop will look something like: every few seconds, read files not ending in .partial (these are currently being written to), store the metadata in a table
	so wal_manager.go scans the directory, ignores invalid files, writes the metadata to the table


todo, decide where to put the table (it's on primary rn)









- the system produces base snapshots (pg_basebackup), captures physical wal segments, stores them somewhere, restores them by replaying wal into a restore target
- primary
	- WAL archiving enabled
	- Will have a physical replication slot
	- Will be used by pg_basebackup
	- Will stream full WAL segments
	- this is not a publisher

- standby
	- Follows primary using physical streaming
	- Helps you test timelines and failover
	- Useful for verifying restore correctness
	- this is not a subscriber (that would be logical replication). it's a hot standby which uses physical streaming replication
	- pg_basebackup intializes the standby

- wal capture service
	- not a pg server, it's a go script doing wal capture

- restore_runner
	- should not run pg until the restore time
	- this is a container only brough up during restore time

- backup/restore
	- You bring this up only when doing a restore
	- You stop it, wipe its data dir, run restore, replay WAL
	- Never needs replication slots (it doesn’t subscribe to anything)

I need 2 physical replciation slots. 1 for pg_basebackup, 1 for the standby
	- inside primary: SELECT * FROM pg_create_physical_replication_slot('pitr_slot');
	- my code will run: pg_receivewal -h localhost -p 5434 -D /wal_archive -U replication_user --slot=pitr_slot
		- this continuously pulls wal segments

there's no decodeing (no wal2json), we use pg_receivewal. pg_receivewal = “download raw WAL segments continuously”

my backup script runs stuff like
pg_basebackup -h localhost -p 5434 \
  -U replication_user \
  -D base_backups/base_2025_11_25 \
  -X stream \
  -C --slot=pitr_slot

then I restore
- Stop restore target container
- Wipe its data dir
- Copy base backup into it
- Add recovery.signal
- Point restore_command to archived WALs
- Start container → PostgreSQL replays WAL until target LSN/time


- dockerfile.wal_capture & sh script
	- wal_catpure isn't a pg server, it's a client container whose only job is to continuously pull raw wal segments from the primary using pg_receivwal. it's just a basica script running something like: pg_receivewal -h pg_primary -p <port> --slot=pitr_slot -D /wal_archive
	- we don't run it in primary to decouple everything. and if primary dies this needs to keep running
	- then it needs a sh script to run it. however i can also write it in go.
	- IMPORTANT: I don't call the sh script. this container is totally separate from all my stuff. the sh script is called when the container starts and ends when the container closes down
		- the db server runs as 1 service
		- the backup agent runs as 1 service
		- they communicate only through replication slots and network connections. 
		- ENTRYPOINT ["/run_wal_capturer.sh"] and my dockerfile.wal_catpure means that docker compose up -d will start the sh script with its container

- dockerfile.postgres
	- makes the pg server image









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


go run file.go
go run .  // runs main.go
