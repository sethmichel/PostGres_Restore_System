primary: data is written here. contains test data table and wal metadata table
standby: for demonstration purposes. has test data table

basic setup overview:
- primary and standby are pg servers. they have a test data table, and primary has a metadata table
- wal_capture container auto runs an sh script which pulls raw wal data and puts it in /wal_archive
- this program reads /wal_archive and updates metadata table
- postgres writes wal data into a fixed size segment that's 16mb or something, so it keeps writting to that until we run out of space. so it's 1 .partial file during that process. .partial means the file is currently being written to. it's removed when teh segment is full and starts a new one
	- this presents a design decision. how do we get data from a file constantly being written to? what do we do with it?
		- for restores, we'll copy the 1 .partial file to a temp location, rename it, and use that. that's a "snapshot" of the db at a point in time.
		- when a file stops being .partial, we can then add its data to the metadata table

**What I've done so far**
Other processes
	- when docker starts, the wal_capture container will start which will trigger the run_wal_capturer.sh to run. this file pulls raw wal data so long as the container is up and ends up putting it in /wal_archive
	- data generator can run separatly and put data in primary test_data

main_process
- load configs
- check files/dirs are setup 
- check db tables exist on primary, standby, and wal metadata table on primary
- check there's 2 physical replication slots active on primary
- create a wal manager object housing the db connection to primary. the process then runs every 5 seconds
	- scans the wal_archive dir for new files that are done loading. updates the metadata table with any new info


**TODO:**
- wal_manager basically updates the metadata table with the partial file size, and then we don't really need to extract any data from it, just the files various status. to do this 
	- wal_manager updates the db with the new .partial file size each loop. we don't copy the file for this
	- to restore, we take 1 snapshot of the .partial file at that moment. a snapshot is a copy of the file with .partial removed, since it's being written to it might tear at the end but that's ideal because it just makes pg stop there. we feed this to the restore engine. then we delete that copy

- possible bug: it's assuming Standard WAL file: 8 chars timeline + 16 chars segment = 24 chars. it might have problems dealing with .partial files. debug needed



how data is pulled from primary
- When wal_catpure container runs the shell script run_wal_captureer.sh will also run. it runs as long as the container is alive
- the sh schript will pull raw wal segments from the primary using pg_receivwal constantly
	- main.go and wal_capturer share access via a docker volume bind mount. because of this volume mapping, any file pg_receivwal writes to /wal_archive in the container instantly appears in docker_connection/wal_archieve. so open that dir in main.go to get the wal data
- main.go should open the wal_archieve files in a loop to get new info, now process that raw wal info

- the wal data is binary, but I don't convert them. I need them in binary format to do a restore. we instead will parse the filename to extract metadata. main.go should store this metadata in a table so the system knows what we have. so the main.go loop will look something like: every few seconds, read files not ending in .partial (these are currently being written to), store the metadata in a table
	so wal_manager.go scans the directory, ignores invalid files, writes the metadata to the table



- the system produces base snapshots (pg_basebackup), captures physical wal segments, stores them somewhere, restores them by replaying wal into a restore target

Containers
- primary container
	- pg server
	- WAL archiving enabled
	- has 2 physical replication slots (1 for pg_basebackup, 1 for the standby)
	- this is not a publisher

- standby container
	- pg server
	- Follows primary using physical streaming
	- Helps test timelines and failover
	- Useful for verifying restore correctness
	- this is not a subscriber (that would be logical replication). it's a hot standby which uses physical streaming replication

- pgadmin container
	- just for viewing the pg servers

- restore target container
	- container is alive all the time, and has a pg server, but it's doing nothing until we restore
	- on restore, we're writing the wal data to this pg server in recovery mode. 
	- once done it promotes from recovery mode to normal mode - it's now a normal pg server

- wal capturer container
	- it's a go script doing wal capture on primary and sending the data to my program

I need 2 physical replciation slots. 
	- inside primary: SELECT * FROM pg_create_physical_replication_slot('pitr_slot');
	- my code will run: pg_receivewal -h localhost -p 5434 -D /wal_archive -U replication_user --slot=pitr_slot
		- this continuously pulls wal segments

files
- data_generator.go
	- generates random data to the primary test_data table every 1 second. this is so we have wal data
	- run this alongside the main program

- run_wal_captureer.sh
	- auto called when teh wal_capturer container is running. this is what gets the data to the program

- rebuild_og_servers.sh
	- nukes containers, deletes volumes, builds containers, launches containers (this also causes the pg servers to be created). run this and everything should be refreshed and usable 

- sql_commands.go
	- houes all the larger sql commands, just so they're in one place

- config.go
	- loads in env files and app configs and stuff

- main.go
	- checks startup configs, everything exists...
	- main data loop (starts the wal_manager loop)
	- gives user a menu to choose actions from
		- generate: start data generation
		- restore: Trigger a Full Restore to Restore Target
		- backup: Trigger a new Base Backup on Primary

- wal_manager.go
	- wal_manager struct updates the primary metadata table every few seconds by looking at the wal_archive folder data. it updates file sizes and general file info. that's all it does

- backup_manager.go
	- gets a snapshot of the wal data at a point in time. we save this as a backup to be used elsewhere

- restore_manager.go
	- snapshot the current db state and restore a new pg server to this state

required files blocked by gitignore
- docker_connection/
	- primary/standby/restore_runner/wal_capture_service.env
	- serviers.json (pg server info)
	- docker-compose.yml
- app.env


How a restore actually works
	if the user selects "restore" in main.go, it will go to restore_manager.go. 
	1) there it snapshots the current state of the db to include the .partial file. we'll use this snapshot to restore a new pg server to.

	2) next, it wipes /var/lib/postgresql/data/* in the restore_target container, copies the base backup (not the step 1 snapshot) to that folder, and checks our permissions. if we haven't run backup yet, it can't do this

	3) configures the recovery mode of restore_target container. creates recovery.signal in "/var/lib/postgresql/data/recovery.signal" on the container (empty file that just tells it to enter recovery mode), now pg will look for the restore command to start restoring: the 1st echo gives that, the 2nd echo tells it to promote itself out of recovery mode and into a normal interactable pg server/container (read/write server).

	4) start the pg server in the container. its' already running but we're doing a new process. the containers default state should be "sleep infinity" so it's alive but not running pg. so we spawn a new pg process on it 

	- base backup: backup command in main(). a complete copy of the db files (base, global, pg_wal, ...) at a point in time. it's in /backups/latest
	- wal snapshot: restore command in main(). a copy of the single .partial wal file. saved in /wal_archive. 

	Keep in mind that since the pg server in estore_target is inactive until a restore, we can see in pgadmin, but it'll be disconnected. it should be fully useable the same ways as primary after a restore - however doing it this way also makes it inherite the credentials of primary. so the username/pw of it are the same as primary. this also means it needs the same settings

------------------------------


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



**Required private local files not in this repo & misc required changes**
- primary, standby, app, restore_runner, wal_capture_service env files, which are the config files
- servers.json which has the info to make the pg servers
- docker-compose.yml file
- docker_connections/wal_archive/ directory
- my version of go writes to the debug console which is read only. you'll need to tell the debugger to run inside the terminal instead

