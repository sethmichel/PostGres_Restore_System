package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// handles scanning and cataloging WAL files
type WalManager struct {
	ArchiveDir string
	DbConn     *pgx.Conn
}

// holds file and LSN info
type WalLsnInfo struct {
	FileName string
	StartLSN string
}

// creates & return a new manager
// connects to primary
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
// files ending in .partial are still being written to, we take a snapshot of these
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
		query := Update_Wal_MetaData_Table()

		result, err := wm.DbConn.Exec(ctx, query, cleanName, timeline, segment, isPartial, size)
		if err != nil {
			log.Printf("Failed to upsert WAL metadata for %s: %v", name, err)
		} else {
			if result.RowsAffected() > 0 {
				updatedCount++
			}
		}
	}

	return updatedCount, nil
}

// returns a list of WAL files and their start LSNs
func (wm *WalManager) GetAvailableLSNs() ([]WalLsnInfo, error) {
	ctx := context.Background()
	rows, err := wm.DbConn.Query(ctx, "SELECT file_name FROM wal_metadata ORDER BY file_name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []WalLsnInfo
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			continue
		}
		lsn, err := CalculateLsnFromFilename(filename)
		if err == nil {
			results = append(results, WalLsnInfo{
				FileName: filename,
				StartLSN: lsn,
			})
		}
	}
	return results, nil
}

// computes the start LSN of a WAL segment
func CalculateLsnFromFilename(filename string) (string, error) {
	// Standard WAL file: 8 chars timeline + 16 chars segment = 24 chars
	if len(filename) != 24 {
		return "", fmt.Errorf("invalid filename length")
	}
	// skip timeline (first 8)
	segmentHex := filename[8:] // 16 chars

	// Parse as uint64
	segVal, err := strconv.ParseUint(segmentHex, 16, 64)
	if err != nil {
		return "", err
	}

	logId := uint32(segVal >> 32)
	segId := uint32(segVal & 0xFFFFFFFF)

	// Calculate LSN
	// LSN = (logId * 4GB) + (segId * 16MB)
	// 4GB = 0x100000000
	// 16MB = 0x1000000
	lsn := (uint64(logId) * 0x100000000) + (uint64(segId) * 0x1000000)

	// Format as X/Y (hex/hex)
	return fmt.Sprintf("%X/%X", uint32(lsn>>32), uint32(lsn&0xFFFFFFFF)), nil
}

// starts a ticker for every x seconds. it's not a stopwatch, it's a signal sender
/*
the loop detects that the .partial file has grown, and it updates the file_size_bytes in my database.
When the file is eventually done (renamed), the loop detects is_partial changed from true to false and
updates that status.
*/
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
