package sql_commands

func Create_Test_Data_Table() string {
	return `
			CREATE TABLE IF NOT exists test_data (
			counter INTEGER,
			message TEXT,
			value NUMERIC
		);
	`
}

func Create_Wal_Metadata_Table() string {
	return `
		CREATE TABLE IF NOT EXISTS wal_metadata (
			file_name TEXT PRIMARY KEY,
			timeline_id INTEGER,
			lsn_start_hex TEXT,
			is_partial BOOLEAN DEFAULT FALSE,
			file_size_bytes BIGINT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			processed BOOLEAN DEFAULT FALSE
		);
	`
}

func Update_Wal_MetaData_Table() string {
	return `
			INSERT INTO wal_metadata (file_name, timeline_id, lsn_start_hex, is_partial, file_size_bytes, processed)
			VALUES ($1, $2, $3, $4, $5, FALSE)
			ON CONFLICT (file_name) DO UPDATE 
			SET is_partial = EXCLUDED.is_partial,
			    file_size_bytes = EXCLUDED.file_size_bytes,
                lsn_start_hex = EXCLUDED.lsn_start_hex
			WHERE 
                (wal_metadata.is_partial = TRUE AND EXCLUDED.is_partial = FALSE) 
                OR 
                (wal_metadata.file_size_bytes != EXCLUDED.file_size_bytes);
		    `
}
