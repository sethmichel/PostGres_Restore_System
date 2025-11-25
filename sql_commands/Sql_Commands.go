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
