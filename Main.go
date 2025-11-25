package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"pg_restore/config"
	"pg_restore/sql_commands"

	"github.com/jackc/pgx/v5"
)

// return a dsn string
func MakeDsn(pg *config.PgConnInfo) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
		pg.Host, pg.Port, pg.User, pg.Password, pg.DbName)
}

// CreateTestDataTableSql returns the SQL to create the test table
func CreateTestDataTableSql() string {
	return sql_commands.Create_Test_Data_Table()
}

// CheckTestDataTable creates test data table on primary/standbys
func CheckTestDataTable(dsn string, serverName string) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Printf("Failed to connect to %s database: %v", serverName, err)
		return
	}
	defer conn.Close(ctx)

	sqlCommand := CreateTestDataTableSql()

	// Create table
	_, err = conn.Exec(ctx, sqlCommand)
	if err != nil {
		log.Printf("Error creating test_data table on %s: %v", serverName, err)
		return
	}

	// Check if row exists
	var count int
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM test_data").Scan(&count)
	if err != nil {
		log.Printf("Error counting rows on %s: %v", serverName, err)
		return
	}

	if count == 0 {
		_, err = conn.Exec(ctx, "INSERT INTO test_data (counter, message, value) VALUES (0, 'Initial row', 0.00)")
		if err != nil {
			log.Printf("Error inserting initial row on %s: %v", serverName, err)
		} else {
			fmt.Printf("test_data table created on %s with initial row\n", serverName)
		}
	} else {
		fmt.Printf("test_data table ready on %s\n", serverName)
	}
}

// CheckDockerConnections verifies required files exist
func CheckDockerConnections() {
	dockerDir := "Docker_Connections"

	// Check if folder exists
	if _, err := os.Stat(dockerDir); os.IsNotExist(err) {
		err := os.Mkdir(dockerDir, 0755)
		if err != nil {
			log.Fatalf("Error creating Docker_Connections folder: %v", err)
		}
		fmt.Printf("Created Docker_Connections folder at %s\n", dockerDir)
		return
	}

	// Check for required files
	requiredFiles := []string{"docker-compose.yml", "Primary.env", "Standby.env"}
	var missingFiles []string

	for _, file := range requiredFiles {
		path := filepath.Join(dockerDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			missingFiles = append(missingFiles, file)
		}
	}

	if len(missingFiles) > 0 {
		fmt.Println("Error: The following required files are missing from Docker_Connections folder:")
		for _, file := range missingFiles {
			fmt.Printf("  - %s\n", file)
		}
		os.Exit(1)
	}

	// Check if app.env exists
	if _, err := os.Stat("app.env"); os.IsNotExist(err) {
		fmt.Println("Error: app.env is missing")
		os.Exit(1)
	}
}

func main() {
	// 1. Check folders and files
	CheckDockerConnections()

	// 2. Load env files
	primaryConfig, err := config.LoadDockerEnvConfig("Primary.env")
	if err != nil {
		log.Fatalf("Failed to load Primary config: %v", err)
	}

	standbyConfig, err := config.LoadDockerEnvConfig("Standby.env")
	if err != nil {
		log.Fatalf("Failed to load Standby config: %v", err)
	}

	// Using wal_capture_service.env as the Sink replacement based on ports
	sinkConfig, err := config.LoadDockerEnvConfig("wal_capture_service.env")
	if err != nil {
		log.Fatalf("Failed to load Sink/WalCapture config: %v", err)
	}

	appConfig, err := config.LoadAppEnvConfig("app.env", primaryConfig)
	if err != nil {
		log.Fatalf("Failed to load App config: %v", err)
	}

	// 3. Make DSN strings
	primaryDsn := MakeDsn(primaryConfig)
	standbyDsn := MakeDsn(standbyConfig)
	sinkDsn := MakeDsn(sinkConfig)

	// Debug output to verify
	fmt.Printf("Loaded Configs. Primary Host: %s:%d\n", primaryConfig.Host, primaryConfig.Port)
	fmt.Printf("App Config: Publication=%s, Slot=%s\n", appConfig.PublicationName, appConfig.SlotName)
	fmt.Printf("Sink DSN: %s\n", sinkDsn)

	// 4. Check database tables
	// We add a small sleep to ensure containers are ready if we just started them (optional)
	time.Sleep(500 * time.Millisecond)

	CheckTestDataTable(primaryDsn, "primary")
	CheckTestDataTable(standbyDsn, "standby")

	// Placeholder for sink table check if needed
	// CheckTestDataTable(sinkDsn, "sink")

	fmt.Println("Startup checks complete.")

	/*
		// Future Implementation from Python lines 205+:
		Create_Cdc_Table(sink_dsn)
		Get_Lsn_Table_Conn(app_config.offsets_path)
		Check_Publication(primary_dsn, app_config.publication_name)
		Check_Subscription(standby_dsn, primary_config, app_config)
		Check_Replication_Slot(primary_dsn, app_config.slot_name, app_config.plugin)
		...
	*/
}
