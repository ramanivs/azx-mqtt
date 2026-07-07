package main

import (
	"database/sql"
	"fmt"
	"os"
	"sync"

	_ "github.com/go-sql-driver/mysql"
)

// DB connection settings — equivalent to the private static properties
// on the PHP Database class.
const (
	dbName     = "inam-copy"
	dbHost     = "172.16.50.166"
	dbPort     = 16306
	dbUsername = "inam_admin"
	dbPassword = "Azx12345"
)

var (
	dbConn *sql.DB
	dbMu   sync.Mutex
)

// ConnectDB mirrors Database::connect() — lazily creates a single shared
// *sql.DB the first time it's called and reuses it afterwards.
// (database/sql pools connections internally, so this single handle is
// safe to share across goroutines/message handlers, unlike PHP's mysqli
// object which is not.)
func ConnectDB() *sql.DB {
	dbMu.Lock()
	defer dbMu.Unlock()

	if dbConn != nil {
		return dbConn
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		dbUsername, dbPassword, dbHost, dbPort, dbName)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		recordError("Could not prepare the MySQL connection: %v", err)
		os.Exit(1)
	}
	if err := conn.Ping(); err != nil {
		recordError("Could not connect to MySQL at %s:%d using database %s: %v", dbHost, dbPort, dbName, err)
		os.Exit(1)
	}

	dbConn = conn
	return dbConn
}

// DisconnectDB mirrors Database::disconnect().
func DisconnectDB() {
	dbMu.Lock()
	defer dbMu.Unlock()
	if dbConn != nil {
		dbConn.Close()
		dbConn = nil
	}
}
