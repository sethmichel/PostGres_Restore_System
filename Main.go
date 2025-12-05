package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// loads env file data
func LoadAllConfigs() (*PgConnInfo, *PgConnInfo, *PgConnInfo, *PgConnInfo, *AppConfig) {
	primaryConfig, err := LoadDockerEnvConfig("Primary.env")
	if err != nil {
		log.Fatalf("Failed to load Primary config: %v", err)
	}

	standbyConfig, err := LoadDockerEnvConfig("Standby.env")
	if err != nil {
		log.Fatalf("Failed to load Standby config: %v", err)
	}

	walCaptureConfig, err := LoadDockerEnvConfig("wal_capture_service.env")
	if err != nil {
		log.Fatalf("Failed to load Sink/WalCapture config: %v", err)
	}

	restoreTargetConfig, err := LoadDockerEnvConfig("Restore_Runner.env")
	if err != nil {
		log.Fatalf("Failed to load Restore Target config: %v", err)
	}

	appConfig, err := LoadAppEnvConfig("app.env", primaryConfig)
	if err != nil {
		log.Fatalf("Failed to load App config: %v", err)
	}

	return primaryConfig, standbyConfig, walCaptureConfig, restoreTargetConfig, appConfig
}

// do various startup checks
func PerformStartupChecks(primary_config *PgConnInfo, standby_config *PgConnInfo, wal_captureer_config *PgConnInfo, restore_target_config *PgConnInfo, app_config *AppConfig) {
	// 1. check files and dir's are setup
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
	if _, err := os.Stat(walArchiveDir); os.IsNotExist(err) {
		os.MkdirAll(walArchiveDir, 0755)
	}

	// check backups folder exists
	backupsDir := filepath.Join("Docker_Connections", "backups")
	if _, err := os.Stat(backupsDir); os.IsNotExist(err) {
		os.MkdirAll(backupsDir, 0755)
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

	sqlCommand := Create_Test_Data_Table()
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

	sqlCommand := Create_Wal_Metadata_Table()
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
	do_we_have_backup := CheckForExistingBackup()

	// 1 load configs
	primaryConfig, standbyConfig, walCaptureConfig, restoreTargetConfig, appConfig := LoadAllConfigs()

	// 2 run system checks
	PerformStartupChecks(primaryConfig, standbyConfig, walCaptureConfig, restoreTargetConfig, appConfig)

	// 3. Start WAL Manager (Continuous Monitoring) in a goroutine
	wm, err := NewWalManager(walArchiveDir, primaryConfig.Dsn)
	if err != nil {
		log.Fatalf("Failed to initialize WAL Manager: %v", err)
	}
	defer wm.Close()

	// Run the WAL monitor in a separate goroutine
	go wm.RunMonitor(5 * time.Second)

	// 4. Interactive CLI Loop
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("\n--- PG Restore System Running ---")
	fmt.Println("Commands:")
	fmt.Println("  backup  - Trigger a new Base Backup on Primary (save a snapshot of the db at this point in time)")
	fmt.Println("  restore - Trigger a Full Restore to Restore Target")
	fmt.Println("  generate - Run Data Generator")
	fmt.Println("  q       - Quit")

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())

		switch input {
		case "generate":
			go DataGeneratorMain()
			fmt.Println("Data Generator started in background...")

		case "backup":
			fmt.Println("")
			err := TriggerBaseBackup("pg_primary")
			if err != nil {
				fmt.Printf("Backup Error: %v\n", err)
			} else {
				do_we_have_backup = true
			}

		case "restore":
			if do_we_have_backup {
				fmt.Println("")
				err := PerformRestore("restore_target", walArchiveDir)
				if err != nil {
					fmt.Printf("Restore Error: %v\n", err)
				}
			} else {
				fmt.Printf("Restore Error: you have to do at least 1 backup before restoring")
			}

		case "q", "quit", "exit":
			fmt.Println("")
			fmt.Println("Shutting down...")
			return

		default:
			fmt.Printf("Unknown command: %q. Available: backup, restore, q\n", input)
		}
	}
}
