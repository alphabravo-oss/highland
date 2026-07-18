package audit

// Register the pure-Go pgx database/sql driver for production durable audit.
import (
	_ "github.com/jackc/pgx/v5/stdlib"
)
