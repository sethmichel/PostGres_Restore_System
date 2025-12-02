**vocab**
- "cluster": a single pg insteance + the db it managers + all its internal files. so pgdata, wal files, system stuff, all db's in it, config files, tables, indexes... so it's 1 pg server instance

- Pg_basebackup

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


**bugs**
FIXED: bug: the extractor container keeps exiting on launch. maybe since there's nothing to extract?
	- primary pg_hba config: added "host replication all all scram-sha-256" this'll let any host to connect for replication using passwords
	- the sh script was using "--create-slot" this might be making the slot and then exiting. maybe it's not combined with the streaming mode correctly
	- fix: create the slow separatly, ignore the error if it already exists, then run the loop