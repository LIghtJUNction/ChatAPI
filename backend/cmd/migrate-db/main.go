package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/zyf2007/ChatAPI/internal/ops/migratedb"
)

func main() {
	sqlitePath := flag.String("sqlite", "", "source SQLite database")
	postgresDSN := flag.String("postgres-dsn", "", "empty PostgreSQL destination DSN")
	flag.Parse()
	if *sqlitePath == "" || *postgresDSN == "" {
		fmt.Fprintln(os.Stderr, "--sqlite and --postgres-dsn are required")
		os.Exit(2)
	}
	report, err := migratedb.SQLiteToPostgres(context.Background(), *sqlitePath, *postgresDSN)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
}
