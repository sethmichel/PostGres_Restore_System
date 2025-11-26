package wal_manager

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"pg_restore/Sql_Commands"

	"github.com/jackc/pgx/v5"
)


// WalManager handles scanning and cataloging WAL files
type WalManager struct {
	ArchiveDir string
	DbConn     *pgx.Conn
}


// creates a new manager
// Assumes metadata table already exists
func NewWalManager(archiveDir string, dsn string) (*WalManager, error) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %v", err)
	}

	return &WalManager{
		ArchiveDir: archiveDir,
		DbConn:     conn,
	}, nil
}


// Close closes the db connection
func (wm *WalManager) Close() {
	if wm.DbConn != nil {
		wm.DbConn.Close(context.Background())
	}
}


// extracts info from the filename
// Standard WAL file: 8 chars timeline + 16 chars segment = 24 chars
// Example: 000000010000000000000001
func ParseWalFilename(filename string) (int, string, bool) {
	if len(filename) != 24 {
		return 0, "", false
	}

	// check if it's hex
	// 24 hex chars is 96 bits, too big for ParseUint 64.
	for _, c := range filename {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return 0, "", false
		}
	}

	timelineStr := filename[:8]
	timeline, err := strconv.ParseInt(timelineStr, 16, 64)
	if err != nil {
		return 0, "", false
	}

	// Segment part (last 16 chars)
	segmentHex := filename[8:]

	return int(timeline), segmentHex, true
}


// scans the directory and updates the metadata table
// Returns number of new/updated files
// files ending in .partial are still being written to, we ignore those
func (wm *WalManager) SyncWalFiles() (int, error) {
	entries, err := os.ReadDir(wm.ArchiveDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read archive dir: %v", err)
	}

	ctx := context.Background()
	updatedCount := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		isPartial := strings.HasSuffix(name, ".partial")
		cleanName := name
		if isPartial {
			cleanName = strings.TrimSuffix(name, ".partial")
		}

		// Skip non-WAL files (.history, .backup, or random files)
		// We strictly look for 24-char hex names
		timeline, segment, valid := ParseWalFilename(cleanName)
		if !valid {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			log.Printf("Error getting file info for %s: %v", name, err)
			continue
		}
		size := info.Size()

		// Upsert into DB
		query := sql_commands.Update_Wal_MetaData_Table()

		cmdTag, err := wm.DbConn.Exec(ctx, query, cleanName, timeline, segment, isPartial, size)
		if err != nil {
			log.Printf("Failed to upsert WAL metadata for %s: %v", name, err)
		} else {
			if cmdTag.RowsAffected() > 0 {
				updatedCount++
			}
		}
	}

	return updatedCount, nil
}


// starts a ticker for every x seconds. it's not a stopwatch, it's a signal sender
func (wm *WalManager) RunMonitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	fmt.Printf("Starting WAL Monitor on %s (Interval: %s)\n", wm.ArchiveDir, interval)

	// ticker.C is the channel the ticker uses to send the signal
	// this means this is an infinite loop with a delay (iterval)
	for range ticker.C {  
		count, err := wm.SyncWalFiles()
		if err != nil {
			log.Printf("Error syncing WAL files: %v", err)
		} else if count > 0 {
			log.Printf("WAL Sync: Updated/Inserted %d records", count)
		}
	}
}

