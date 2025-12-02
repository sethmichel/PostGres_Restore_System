package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"pg_restore/config"
	"pg_restore/sql_commands"
	"pg_restore/wal_manager"
	"time"

	"github.com/jackc/pgx/v5"
)

// loads env file data
func LoadAllConfigs() (*config.PgConnInfo, *config.PgConnInfo, *config.PgConnInfo, *config.PgConnInfo, *config.AppConfig) {
	primaryConfig, err := config.LoadDockerEnvConfig("Primary.env")
	if err != nil {
		log.Fatalf("Failed to load Primary config: %v", err)
	}

	standbyConfig, err := config.LoadDockerEnvConfig("Standby.env")
	if err != nil {
		log.Fatalf("Failed to load Standby config: %v", err)
	}

	walCaptureConfig, err := config.LoadDockerEnvConfig("wal_capture_service.env")
	if err != nil {
		log.Fatalf("Failed to load Sink/WalCapture config: %v", err)
	}

	restoreTargetConfig, err := config.LoadDockerEnvConfig("Restore_Runner.env")
	if err != nil {
		log.Fatalf("Failed to load Restore Target config: %v", err)
	}

	appConfig, err := config.LoadAppEnvConfig("app.env", primaryConfig)
	if err != nil {
		log.Fatalf("Failed to load App config: %v", err)
	}

	return primaryConfig, standbyConfig, walCaptureConfig, restoreTargetConfig, appConfig
}

// do various startup checks
func PerformStartupChecks(primary_config *config.PgConnInfo, standby_config *config.PgConnInfo, wal_captureer_config *config.PgConnInfo, restore_target_config *config.PgConnInfo, app_config *config.AppConfig) {
	// 1. check files and dir's are present
	CheckDirsFiles()
	// DEBUG
	fmt.Printf("Loaded Configs. Primary Host: %s:%d\n", primary_config.Host, primary_config.Port)

	// 2. Database Tables (Test Data)
	CheckTestDataTable(primary_config.Dsn, "primary") // Database Tables
	CheckTestDataTable(standby_config.Dsn, "standby") // Database Tables
	CheckMetaDataTable(primary_config.Dsn)            // wal metadata table

	// 3. plysical replication slots
	CheckPhysicalReplicationSlots(primary_config.Dsn)

	fmt.Println("All Startup Checks Complete.")
}

func CheckDirsFiles() {
	dockerDir := "Docker_Connections"

	// check if docker dir exists
	_, err := os.Stat(dockerDir)
	if os.IsNotExist(err) {
		if err := os.Mkdir(dockerDir, 0755); err != nil {
			log.Fatalf("Error creating Docker_Connections folder: %v", err)
		}
		fmt.Printf("Created Docker_Connections folder at %s\n", dockerDir)
		return
	}

	// check for required docker files
	requiredFiles := []string{"docker-compose.yml", "Primary.env", "Standby.env", "Restore_Runner.env", "wal_capture_service.env", "Dockerfile.postgres"}
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

	// check app.env exists (diff folder)
	if _, err := os.Stat("app.env"); os.IsNotExist(err) {
		fmt.Println("Error: app.env is missing")
		os.Exit(1)
	}

	// check the wal archive folder exists
	walArchiveDir := filepath.Join("Docker_Connections", "wal_archive")
	if os.Stat(walArchiveDir); os.IsNotExist(err) {
		os.MkdirAll(walArchiveDir, 0755)
	}
}

func CheckTestDataTable(dsn string, serverName string) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Printf("Failed to connect to %s database: %v", serverName, err)
		return
	}
	defer conn.Close(ctx)

	sqlCommand := sql_commands.Create_Test_Data_Table()
	if _, err = conn.Exec(ctx, sqlCommand); err != nil {
		log.Printf("Error creating test_data table on %s: %v", serverName, err)
	}
}

func CheckMetaDataTable(dsn string) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Printf("Failed to connect to Primary for metadata check: %v", err)
		return
	}
	defer conn.Close(ctx)

	sqlCommand := sql_commands.Create_Wal_Metadata_Table()
	if _, err := conn.Exec(ctx, sqlCommand); err != nil {
		log.Fatalf("Error creating wal_metadata table: %v", err)
	}
	fmt.Println("Checked/Created wal_metadata table on Primary.")
}

func CheckPhysicalReplicationSlots(dsn string) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Printf("Failed to connect to primary for slot check: %v", err)
		return
	}
	defer conn.Close(ctx)

	var count int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM pg_replication_slots WHERE slot_type = 'physical' AND active = true").Scan(&count)
	if err != nil {
		log.Printf("Error checking replication slots: %v", err)
		return
	}

	if count == 2 {
		fmt.Printf("Success: Primary has %d active physical replication slots.\n", count)
	} else {
		fmt.Printf("Warning: Primary has %d active physical replication slots (expected 2).\n", count)
	}
}

func main() {
	walArchiveDir := filepath.Join("Docker_Connections", "wal_archive")

	// 1 load configs
	primaryConfig, standbyConfig, walCaptureConfig, restoreTargetConfig, appConfig := LoadAllConfigs()

	// 2 run system checks
	PerformStartupChecks(primaryConfig, standbyConfig, walCaptureConfig, restoreTargetConfig, appConfig)

	// MAIN DATA LOOP

	// 3. Start WAL Manager (Continuous Monitoring)
	wal_manager, err := wal_manager.NewWalManager(walArchiveDir, primaryConfig.Dsn)
	if err != nil {
		log.Fatalf("Failed to initialize WAL Manager: %v", err)
	}
	defer wal_manager.Close()

	// Run the loop (blocking)
	wal_manager.RunMonitor(5 * time.Second)
}
