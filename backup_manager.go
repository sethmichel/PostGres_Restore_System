package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

/*
- does pg_basebackup inside the pg_primary container to get a snapshot of the db
- Backups are stored in the shared /backups/latest directory.
*/

// checks if a backup exists in Docker_Connections/backups/latest
func CheckForExistingBackup() bool {
	// check for postgresql.conf as a sign that the backup exists and is populated
	backupConfigPath := filepath.Join("Docker_Connections", "backups", "latest", "postgresql.conf")
	if _, err := os.Stat(backupConfigPath); err == nil {
		return true
	}
	return false
}

// runs pg_basebackup on primary. this will get a snapshot of the wal at this point in time
func TriggerBaseBackup(primaryContainerName string) error {
	fmt.Println("Starting Base Backup...")

	// command: pg_basebackup -h localhost -p 5432 -U replication_user -D /backups/latest -X stream -F p -v
	// Note: We are running this INSIDE the pg_primary container, so host is localhost (or just default socket)

	// need to clean /backups/latest first if it exists to avoid "directory not empty" error.
	cleanCmd := exec.Command("docker", "exec", primaryContainerName, "rm", "-rf", "/backups/latest")
	if output, err := cleanCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clean backup dir: %s: %w", string(output), err)
	}

	// assuming superuser
	// -X stream: stream WALs
	// -F p: plain format (default)
	// -c fast: fast checkpoint
	cmd := exec.Command("docker", "exec", primaryContainerName,
		"pg_basebackup",
		"-h", "localhost",
		"-U", "primary_user",
		"-D", "/backups/latest",
		"-X", "stream",
		"-F", "p",
		"-v",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_basebackup failed: %s: %w", string(output), err)
	}

	fmt.Printf("Backup completed successfully:\n%s\n", string(output))
	return nil
}
