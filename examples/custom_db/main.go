package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
	manadb "github.com/Aswanidev-vs/mana/storage/db"

	// Import your preferred database driver
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	// ================================================================
	// 1. Connect to your existing Database
	// ================================================================
	// Here we use PostgreSQL, but you could use MySQL or SQLite.
	dsn := "postgres://user:pass@localhost:5432/myapp?sslmode=disable"
	dbConn, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer dbConn.Close()

	// ================================================================
	// 2. Initialize Mana with your existing Database
	// ================================================================
	cfg := core.DefaultConfig()
	cfg.EnableAuth = true
	
	// Optional: Isolate Mana's tables with a prefix to avoid colliding 
	// with your own tables (e.g., this creates 'mana_messages', 'mana_profiles').
	cfg.DatabaseTablePrefix = "mana_"

	app := mana.New(cfg)

	// Instruct Mana to use your `*sql.DB` connection pool. 
	// This automatically wires up the high-performance SQL batteries 
	// for Messaging, Identity, Social, and Settings!
	app.WithDatabase(dbConn, manadb.Postgres)

	// ================================================================
	// 3. Shared Transactions!
	// ================================================================
	// Since Mana is using your DB connection, you can share SQL transactions
	// between your own application logic and Mana's internal stores.

	ctx := context.Background()

	// Begin a transaction on your database
	tx, err := dbConn.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}

	// 1. Do something in YOUR tables
	_, err = tx.ExecContext(ctx, "UPDATE users SET money = money - 10 WHERE id = 1")
	if err != nil {
		tx.Rollback()
		return
	}

	// 2. Inject the transaction into the context so Mana can use it
	txCtx := manadb.WithTx(ctx, tx)

	// 3. Call a Mana function (e.g., SaveMessage).
	// Because we pass the `txCtx`, Mana will execute this INSERT *inside* your `tx`.
	_, err = app.MessageStore().SaveMessage(txCtx, core.Message{
		Type:    "payment_receipt",
		RoomID:  "support-room",
		Payload: []byte("You spent 10 coins!"),
	}, []string{"user-1"})
	
	if err != nil {
		// If Mana fails, your `money = money - 10` is rolled back too!
		tx.Rollback()
		log.Fatalf("mana failed, transaction rolled back: %v", err)
	}

	// Commit both your update and Mana's insert atomically
	err = tx.Commit()
	if err != nil {
		log.Fatalf("commit failed: %v", err)
	}

	fmt.Println("Successfully saved a message and updated custom tables within a single atomic transaction!")
}
