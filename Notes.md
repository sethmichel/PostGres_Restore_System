***TODO***
- test backup process
- test the restore process
- might have bugs around the pg server in restore_target
- make sure restore_target auto makes a pg server like primary does
Later
	- restore to a timestamp
	- restore to a lsn


**vocab**
- "cluster": a single pg insteance + the db it managers + all its internal files. so pgdata, wal files, system stuff, all db's in it, config files, tables, indexes... so it's 1 pg server instance

- Pg_basebackup aka base snapshot: the starting image of the pg cluster that we recover from. so it's our snapshot we're using for restores.

- Pg_recvlogical


**commands**
run docker setup
docker compose down -v # nukes it

docker compose build --no-cache

docker compose up -d


go run file.go
go run .  // runs main.go


**concepts**
- pgadmin is just a viewer, what you do is load the references to existing servers even though it's its own container
- go contexts
- go recievers (when we write a type before the function name (wal_manager.go))
- go channels
- go build requires the package & function to be named main
- go packages
	- all files in a direcotry must belong to teh same package. since main.go is the start point and uses func main(), all other files must use package main
- go brackets { }
	- it won't break the code to put the backet on the next line, but it's super bad to do. it just can cause issues and it's universally regarded as bad practice


**bugs**
FIXED: the extractor container keeps exiting on launch. maybe since there's nothing to extract?
	- primary pg_hba config: added "host replication all all scram-sha-256" this'll let any host to connect for replication using passwords
	- the sh script was using "--create-slot" this might be making the slot and then exiting. maybe it's not combined with the streaming mode correctly
	- fix: create the slow separatly, ignore the error if it already exists, then run the loop

FIXED: nothing being written to wal_archive
	- might be primary is trying to archive data (archive_mode=on) while the service is trying to access the same dir, so they're fighting for access. 
	- OR things are ending in crlf line endings. so we can add a line to dockerfile.wal_captureer to covert these

IGNORING: Since this is a physical restore, the restored server will inherit the same credentials as the Primary (User: primary_user, Pass: primary_pw), effectively ignoring the POSTGRES_USER variables in Restore_Runner.env.



**weird things I've seen**
- multiple raw wal files can have the same timeline id. the timeline id is the first 8 chars, so hundreds of thousands will have the same id (1) until a failure or restore event makes a new timeline (2). the 16 chars after that is the unique sequence number of the file in that timeline (segment_number)