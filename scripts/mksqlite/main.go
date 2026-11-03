// Command mksqlite creates or corrupts a sample SQLite database for
// scripts/demo.sh.
//
//	go run ./scripts/mksqlite path/to/db.sqlite3          create
//	go run ./scripts/mksqlite -corrupt path/to/db.sqlite3 corrupt in place
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	corrupt := flag.Bool("corrupt", false, "overwrite the middle of an existing database with garbage")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: mksqlite [-corrupt] path")
		os.Exit(2)
	}
	var err error
	if *corrupt {
		err = corruptDB(flag.Arg(0))
	} else {
		err = createDB(flag.Arg(0))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mksqlite:", err)
		os.Exit(1)
	}
}

// createDB writes a small vault-like database spanning many pages.
func createDB(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA journal_mode=DELETE; CREATE TABLE ciphers (id INTEGER PRIMARY KEY, name TEXT, data TEXT); CREATE INDEX ciphers_name ON ciphers(name)"); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for i := 0; i < 2000; i++ {
		if _, err := tx.Exec("INSERT INTO ciphers (name, data) VALUES (?, ?)", fmt.Sprintf("login-%04d", i), fmt.Sprintf("%0200d", i)); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// corruptDB overwrites 4 KiB in the middle of the file, keeping the header
// intact so SQLite still opens it but integrity_check fails.
func corruptDB(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < 16384 {
		return fmt.Errorf("%s is too small to corrupt", path)
	}
	for i := len(data) / 2; i < len(data)/2+4096; i++ {
		data[i] = 0xA5
	}
	return os.WriteFile(path, data, 0o644)
}
