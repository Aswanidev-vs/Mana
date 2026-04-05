package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	mana "github.com/Aswanidev-vs/mana"
	"github.com/Aswanidev-vs/mana/core"
	manadb "github.com/Aswanidev-vs/mana/storage/db"
	_ "modernc.org/sqlite"
)

func main() {
	// ================================================================
	// 1. Connect to your existing Database (e.g. SQLite)
	// ================================================================
	dbPath := "data/custom_app.db"
	os.MkdirAll("data", 0755)
	
	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer dbConn.Close()

	// Create a dummy "users" table for our custom app logic
	_, _ = dbConn.Exec("CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, balance INTEGER)")
	_, _ = dbConn.Exec("INSERT OR IGNORE INTO users (id, balance) VALUES ('alice', 100)")

	// ================================================================
	// 2. Initialize Mana with your existing Database
	// ================================================================
	cfg := core.DefaultConfig()
	cfg.EnableAuth = true
	cfg.DatabaseTablePrefix = "mana_" // Isolate framework tables

	app := mana.New(cfg)

	// Instruct Mana to use your existing *sql.DB connection.
	// This wires up all framework stores (Messages, Accounts, etc.) to your DB.
	app.WithDatabase(dbConn, manadb.SQLite)

	// ================================================================
	// 3. Shared Transactions Case Study: Nuclear Atomicity!
	// ================================================================
	// Scenario: A user buys a premium feature. 
	// We want to deduct money AND post a secure message atomically.
	
	ctx := context.Background()
	tx, err := dbConn.BeginTx(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Step A: Deduct balance in YOUR custom table
	log.Println("Step A: Deducting 20 coins from Alice's account...")
	_, err = tx.ExecContext(ctx, "UPDATE users SET balance = balance - 20 WHERE id = 'alice'")
	if err != nil {
		tx.Rollback()
		log.Fatal("Payment failed, rolling back.")
	}

	// Step B: Inject transaction into context so Mana can join it
	txCtx := manadb.WithTx(ctx, tx)

	// Step C: Save a framework message using the SAME transaction
	log.Println("Step B: Saving transaction receipt in Mana message store...")
	_, err = app.MessageStore().SaveMessage(txCtx, core.Message{
		SenderID: "system",
		RoomID:   "u-alice",
		Payload:  []byte("Receipt: -20 coins for Premium Upgrade"),
		Type:     "payment_alert",
	}, []string{"u-alice"})
	
	if err != nil {
		// CRITICAL: If Mana fails, Step A is also rolled back automatically!
		tx.Rollback()
		log.Fatalf("Mana failed to save receipt, payment rolled back: %v", err)
	}

	// Step D: Commit everything 
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("--- ATOMIC SUCCESS ---")
	fmt.Println("1. 20 coins deducted from your custom 'users' table.")
	fmt.Println("2. Receipt message saved in framework 'mana_messages' table.")
	fmt.Println("Both happened in one single SQL transaction.")
}
