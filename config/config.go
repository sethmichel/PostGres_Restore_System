package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

// PgConnInfo holds postgres connection parameters
type PgConnInfo struct {
	Host     string
	HostName string
	Port     int
	User     string
	Password string
	DbName   string
	Dsn      string
}

// AppConfig holds application settings
type AppConfig struct {
	Primary               *PgConnInfo
	SlotName              string
	Plugin                string // pgoutput or wal2json
	StartFromBeginning    bool
	BatchSize             int
	MaxRetries            int
	BackoffSeconds        float64
	StatusIntervalSeconds float64
	OffsetsPath           string
}

func MakeDsn(pg *PgConnInfo) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
		pg.Host, pg.Port, pg.User, pg.Password, pg.DbName)
}

// loads database connection info from the .env files
// this is also called from the data generator which messes up the directory. so we make sure everythings right first
func LoadDockerEnvConfig(envFile string) (*PgConnInfo, error) {
	// Try to find Docker_Connections in current or parent directory
	envPath := filepath.Join("Docker_Connections", envFile)
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		// Try parent directory
		parentPath := filepath.Join("..", "Docker_Connections", envFile)
		if _, err := os.Stat(parentPath); err == nil {
			envPath = parentPath
		}
	}

	// overload: load in data and overwrite existing data
	err := godotenv.Overload(envPath)
	if err != nil {
		return nil, fmt.Errorf("error loading .env file %s: %w", envPath, err)
	}

	// port mapping: host port : container port
	portMapping := map[string]int{
		"Primary.env":             5434,
		"Standby.env":             5435,
		"Wal_Capture_Service.env": 5436,
		"Restore_Runner.env":      5437,
	}

	hostPort, ok := portMapping[envFile] // ok is a bool telling me if the key exists in the map
	if !ok {
		// fallback to env file if not in map
		p, err := strconv.Atoi(os.Getenv("POSTGRES_PORT"))
		if err != nil {
			return nil, fmt.Errorf("unknown env file and POSTGRES_PORT not set/valid for: %s", envFile)
		}
		hostPort = p
	}

	connInfo := &PgConnInfo{
		Host:     "localhost",
		HostName: os.Getenv("HOST_NAME"),
		Port:     hostPort,
		User:     os.Getenv("POSTGRES_USER"),
		Password: os.Getenv("POSTGRES_PASSWORD"),
		DbName:   os.Getenv("POSTGRES_DB"),
	}
	connInfo.Dsn = MakeDsn(connInfo)

	if connInfo.HostName == "" || connInfo.User == "" || connInfo.Password == "" || connInfo.DbName == "" {
		return nil, fmt.Errorf("missing environment variables in file: %s", envFile)
	}

	return connInfo, nil
}

// load app env file
func LoadAppEnvConfig(envFile string, primaryConfig *PgConnInfo) (*AppConfig, error) {
	err := godotenv.Load(envFile)
	if err != nil {
		return nil, fmt.Errorf("error loading app env file %s: %w", envFile, err)
	}

	batchSize, _ := strconv.Atoi(os.Getenv("batch_size"))
	maxRetries, _ := strconv.Atoi(os.Getenv("max_retries"))
	backoffSeconds, _ := strconv.ParseFloat(os.Getenv("backoff_seconds"), 64)
	statusInterval, _ := strconv.ParseFloat(os.Getenv("status_interval_seconds"), 64)
	startFromBeginning := os.Getenv("start_from_beginning") == "true"

	appInfo := &AppConfig{
		Primary:               primaryConfig,
		SlotName:              os.Getenv("slot_name"),
		Plugin:                os.Getenv("plugin"),
		StartFromBeginning:    startFromBeginning,
		BatchSize:             batchSize,
		MaxRetries:            maxRetries,
		BackoffSeconds:        backoffSeconds,
		StatusIntervalSeconds: statusInterval,
		OffsetsPath:           os.Getenv("offsets_path"),
	}

	return appInfo, nil
}
