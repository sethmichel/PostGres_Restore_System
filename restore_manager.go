package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

/*
- Copies the current .partial WAL file so the restore includes the latest data
- Cleans the restore_target data directory (docker)
- Copies the base backup from /backups/latest
- Creates recovery.signal and sets restore_command to replay WALs from /wal_archive
- Launches the Postgres process inside the restore_target container
*/

// restore process controller
func PerformRestore(restoreContainerName string, walArchiveDir string, targetLSN string) error {
	fmt.Println("Starting Restore Process...")

	// 0. Stop any running Postgres process in the restore_target container
	// This prevents memory leaks
	if err := StopPostgres(restoreContainerName); err != nil {
		// Warning only, as it might not be running
		fmt.Printf("Note: StopPostgres reported: %v\n", err)
	}

	// 1. Snapshot the current .partial WAL file
	if err := SnapshotWal(walArchiveDir); err != nil {
		return fmt.Errorf("failed to snapshot WAL: %w", err)
	}

	// 2. Prepare the data directory (Wipe & Restore Base Backup)
	if err := PrepareDataDir(restoreContainerName); err != nil {
		return fmt.Errorf("failed to prepare data directory: %w", err)
	}

	// 3. Configure Recovery settings
	if err := ConfigureRecovery(restoreContainerName, targetLSN); err != nil {
		return fmt.Errorf("failed to configure recovery: %w", err)
	}

	// 4. Start Postgres inside the container
	if err := StartPostgres(restoreContainerName); err != nil {
		return fmt.Errorf("failed to start postgres: %w", err)
	}

	fmt.Println("Restore initiated successfully. Check container logs.")
	return nil
}

// Finds any .partial file and copies it to a 'ready' file
func SnapshotWal(archiveDir string) error {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".partial") {
			srcPath := filepath.Join(archiveDir, entry.Name())
			destName := strings.TrimSuffix(entry.Name(), ".partial")
			destPath := filepath.Join(archiveDir, destName)

			fmt.Printf("Snapshotting WAL: %s -> %s\n", entry.Name(), destName)
			if err := copyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// Wipes the data directory and copies the base backup
func PrepareDataDir(containerName string) error {
	// 1. Wipe Data Dir
	// We use "bash -c" to handle glob expansion (*)
	fmt.Println("Wiping restore target data directory...")
	wipeCmd := exec.Command("docker", "exec", containerName, "bash", "-c", "rm -rf /var/lib/postgresql/data/*")
	if out, err := wipeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wipe failed: %s: %w", string(out), err)
	}

	// 2. Copy Base Backup
	// Copy content from /backups/latest to /var/lib/postgresql/data/
	fmt.Println("Copying base backup to data directory...")
	copyCmd := exec.Command("docker", "exec", containerName, "bash", "-c", "cp -r /backups/latest/* /var/lib/postgresql/data/")
	if out, err := copyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy backup failed: %s: %w", string(out), err)
	}

	// Ensure correct permissions (postgres user is usually uid 999, but inside container 'postgres' user is best)
	// We run chown just in case
	permCmd := exec.Command("docker", "exec", containerName, "chown", "-R", "postgres:postgres", "/var/lib/postgresql/data")
	if out, err := permCmd.CombinedOutput(); err != nil {
		// Warn but don't fail hard if user doesn't exist in this context (though it should)
		fmt.Printf("Warning: chown output: %s\n", string(out))
	}

	return nil
}

// Writes recovery.signal and postgresql.auto.conf
func ConfigureRecovery(containerName string, targetLSN string) error {
	fmt.Println("Configuring recovery parameters...")

	// 1. Create recovery.signal
	touchCmd := exec.Command("docker", "exec", containerName, "touch", "/var/lib/postgresql/data/recovery.signal")
	if err := touchCmd.Run(); err != nil {
		return err
	}

	// 2. Set restore_command and recovery_target_action
	// Escape quotes for bash execution
	configCmds := []string{
		"echo \"restore_command = 'cp /wal_archive/%f %p'\" >> /var/lib/postgresql/data/postgresql.auto.conf",
		"echo \"recovery_target_action = 'promote'\" >> /var/lib/postgresql/data/postgresql.auto.conf",
	}

	if targetLSN != "" {
		configCmds = append(configCmds, fmt.Sprintf("echo \"recovery_target_lsn = '%s'\" >> /var/lib/postgresql/data/postgresql.auto.conf", targetLSN))
		configCmds = append(configCmds, "echo \"recovery_target_inclusive = 'true'\" >> /var/lib/postgresql/data/postgresql.auto.conf")
	}

	// Executing those shell commands on the container
	for _, cmdStr := range configCmds {
		cmd := exec.Command("docker", "exec", containerName, "bash", "-c", cmdStr)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("config failed: %s: %w", string(out), err)
		}
	}

	return nil
}

// Stops any running postgres process inside the container
func StopPostgres(containerName string) error {
	fmt.Println("Stopping any existing Postgres process in restore target...")
	// We use pkill to find and kill the postgres process
	// If pkill fails (exit code 1), it usually means no process was found, which is fine.
	cmd := exec.Command("docker", "exec", containerName, "pkill", "postgres")
	_ = cmd.Run() // Ignore error as process might not be running

	// Give it a moment to shut down cleanly if it was running
	// In a real scenario, we might want to wait, but for now this is a simple kill
	return nil
}

// Starts the postgres process in the background
func StartPostgres(containerName string) error {
	fmt.Println("Starting Postgres...")
	// We use -d to run in detached mode
	// We run the 'postgres' command.
	// We MUST match the Primary's configuration (especially max_connections=200)
	// or the restore will fail with "insufficient parameter settings"
	cmd := exec.Command("docker", "exec", "-d", containerName, "docker-entrypoint.sh", "postgres",
		"-c", "wal_level=replica",
		"-c", "max_wal_senders=10",
		"-c", "max_replication_slots=5",
		"-c", "max_connections=200",
		"-c", "archive_mode=off",
		"-c", "listen_addresses=*",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start postgres: %s: %w", string(out), err)
	}
	return nil
}
