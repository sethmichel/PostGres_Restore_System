primary: data is written here. contains test data table and wal metadata table
standby: for demonstration purposes. has test data table

basic setup overview:
- primary and standby are pg servers. they have a test data table, and primary has a metadata table
- wal_capture container auto runs an sh script which pulls raw wal data and puts it in /wal_archive
- this program reads /wal_archive and updates metadata table


What I've done so far
- when docker starts, the wal_capture container will start which will trigger the run_wal_capturer.sh to run. this file pulls raw wal data so long as the container is up and ends up putting it in /wal_archive
- load configs
- check files/dirs are setup 
- check db tables exist on primary, standby, and wal metadata table on primary
- check there's 2 physical replication slots active on primary
- create a wal manager object housing the db connection to primary. the subprocess then runs every 5 seconds
	- scans the wal_archive dir for new files that are done loading. updates the metadata table with any new info

**It seems at this stage I have the startup and metadata table updating done
**next steps: 
	- write a script to add test data every 1 second to primary so we have new wal logs to read
		- the wal_manager should see the files being written to /wal_archive




how data is pulled from primary
- When wal_catpure container runs the shell script run_wal_captureer.sh will also run. it runs as long as the container is alive
- the sh schript will pull raw wal segments from the primary using pg_receivwal constantly
	- main.go and wal_capturer share access via a docker volume bind mount. because of this volume mapping, any file pg_receivwal writes to /wal_archive in the container instantly appears in docker_connection/wal_archieve. so open that dir in main.go to get the wal data
- main.go should open the wal_archieve files in a loop to get new info, now process that raw wal info

- the wal data is binary, but I don't convert them. I need them in binary format to do a restore. we instead will parse the filename to extract metadata. main.go should store this metadata in a table so the system knows what we have. so the main.go loop will look something like: every few seconds, read files not ending in .partial (these are currently being written to), store the metadata in a table
	so wal_manager.go scans the directory, ignores invalid files, writes the metadata to the table



- the system produces base snapshots (pg_basebackup), captures physical wal segments, stores them somewhere, restores them by replaying wal into a restore target
- primary
	- pg server
	- WAL archiving enabled
	- Will have a physical replication slot
	- Will be used by pg_basebackup
	- Will stream full WAL segments
	- this is not a publisher

- standby
	- pg server
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




**notes**
there are 2 way pitr can be used
1) You create a new Postgres instance (new cluster) that:
	- initializes a new empty data directory
	- restors base backup
	- replays WAL until the target
	- promotes itself
	this is basically for debugging/inspection. your stuff all still points to the original server, this one is just a copy at a point in time that nothing is connected to

2) If your primary server has catastrophic corruption:
	- you spin up the restore runner
	- it recovers to the right point
	- this restored instance becomes your new primary
	- Then you:
		- shut down the old primary (destroy it)
		- point your apps to the restored instance
		- create a new standby off of it
		- This is disaster recovery.
	now the restored new pg server we made it our new pg server we're using. the original is gone


**milestones**
The goal of this is to make a copy of the primary server at a point in time for the purpose of inspection. we will not reroute components to the copy. the copies purpose is to be created.

1) wal capture service
	- basically my other cdc project. run containerized pg servers, continuously get wal logs from primary pg server by physical replication
	- store them in a local directory or on azure blob
	- maintain state by using lsn offsets which we store in a small local db. this will handle safely restarting without missing data
	- should be restart/crash safe
	- controlled by run_wal_captureer.sh

2) restore service
	- spin up a new pg cluster in docker in recovery mode (so it'll read wal data and replay it) and initialize the cluster (initdb)
		- should apply base backup, apply all archived wal's we're continuosuly saving, go up to a selected lsn or timestamp
		- this will restore the db to any point in time
	- once it's done recovering, the instance promotes itself out of recovery mode and becomes normal usable server right away. so now it can accept read/writes. This is now the copy we can inspect. everything else still point to the original.

3) docker based replication topology
	- Your final Compose environment includes: pg_primary, pg_standby (using streaming replication + WAL shipping), wal_capturer service, restore_runner service, pgadmin for inspection
	each node
		- Has its own environment file
		- Connects through deterministic host ports
		- Logs out detailed replication/WAL diagnostics to your Go program

4) stress test
	- concurrent write generator (5 go routines)
		- Each goroutine injects INSERTs:
			- random intervals
			- random payloads
			- unique ID and timestamp
			- commit timestamp recorded in verification_log.json
		- What this tests:
			- WAL generation under concurrency
			- WAL order preservation
			- race conditions in your WAL capturer
			- high-write throughput
		- This test is essential because WAL is written at commit time — not insert time — and you’re testing WAL order
	
	- Real-Time Chaos Injection
		- Random events every 30–60 seconds:
			- docker restart wal_capturer
			- docker pause pg_primary (simulates network partition)
			- docker pause pg_standby (simulates hot-standby desync)
			- optionally: kill the Go process with SIGKILL
		- This creates a “cloud environment” failure pattern:
			- random crashes
			- unstable network
			- container pause / freeze
			- WAL gaps
			- restart timing races
		- This tests the real robustness of your LSN handling.

	- PITR to a Random Timestamp
		- After chaos finishes:
			- select a random timestamp T from the JSON log
			- run restore process
			- query restored db

	- what it should prove
		- PITR replay is timestamp-accurate
		- WAL capture didn’t lose commit-boundaries
		- WAL capture didn’t reorder writes
		- recoveries match reality to the second
		- recovery_target_time is being used correctly
		- LSN boundaries match timestamp boundaries

5) mangagment notes
the go project includes
	- WAL monitoring loop
		- Uses time.NewTicker(interval)
		- Syncs new WAL files every N seconds
		- Logs and reports metrics
	- Config loader
		- .env file loader for each service
		- Maps services → correct host ports
		- Validates configuration before connecting
	- Docker orchestrator helpers
		- Bootstraps primary
		- Initializes standby
		- Starts and stops WAL capturer
		- Triggers restore workflow
	- SQL table check + create utilities
		- You automatically ensure test tables exist
		- Can verify row counts
		- Can validate integrity

6) need to add cloud storage (AWS S3 or Azure Blob Storage)
    - The Upgrade: Modify WalManager so that after it detects and indexes a new WAL file, it uses the AWS SDK for Go (v2) or Azure SDK to upload that file to a storage bucket.
    - The Restore: When the Restore Engine runs, instead of copying from a local folder, it downloads the specific range of WAL files needed from the cloud.


**Chaos Test**
after it's done add this test
	- Spawns 5-10 concurrent Go routines injecting INSERT statements into the Primary DB.
	- Logs every inserted ID and its exact commit timestamp to a local verification_log.json.
	- Run this in the background: Every 30-60 seconds, it randomly executes docker restart pg_restore_wal_capturer_1 or momentarily pauses the Primary container to simulate network partition/crash.
	- verify:
		- Stops the chaos.
		- Picks a random timestamp $T$ from the verification_log.json.
		- Triggers your Restore Process to recover to time $T$.
		- Queries the Restored DB: "Select count() where created_at <= $T$".
		- Pass Condition: The count in the DB matches exactly the count in your local JSON log for that timestamp.



**files**
- docker-compose.yml
	- the config file for my servers and containers
- dockerfiles
	- one for the databases, one for the restore target, one for the wal_capturer
- rebuild_pg_servers.sh
	- deletes the containers and pg servers, destroys volumes, clears wal_archive
	- only does the following for primary and standby: rebuilds containers, launches them, starts pgadmin which should see servers.json and load the new versions of the db's
- servers.json
	- loaded by the yml file



**Required private local files not in this repo**
- primary, standby, app env files, which are the config files
- servers.json which has the info to make the pg servers


