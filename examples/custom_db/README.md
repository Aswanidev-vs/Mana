# Mana Custom Database Integration Guide

This guide explains how to use the "plug-and-play" SQL batteries we just implemented. By injecting your own database connection, Mana will automatically initialize highly optimized SQL stores for **Messaging**, **Identity**, **Social** (profiles/contacts), and **Settings** using your database structure.

## Overview of the implementation

We have added an example implementation in `main.go` inside this directory. Below is a step-by-step breakdown of how the code works and how you can implement it in your own system.

### Step 1: Connect your Database

First, connect to whichever database you are already using for your app (Postgres, MySQL, or SQLite). 

```go
// Connect to your existing PostgreSQL Database
dsn := "postgres://user:pass@localhost:5432/myapp?sslmode=disable"
dbConn, err := sql.Open("pgx", dsn)
if err != nil {
	log.Fatalf("failed to connect: %v", err)
}
defer dbConn.Close()
```

### Step 2: Configure Mana with Table Prefix isolation

Initialize Mana as you normally would, but be sure to set `DatabaseTablePrefix`. This prepends a string (like `mana_`) to all of Mana's internal tables (so you get `mana_messages` instead of `messages`), guaranteeing it won't conflict with any tables you've already built!

```go
cfg := core.DefaultConfig()
cfg.DatabaseTablePrefix = "mana_" // Highly recommended!
app := mana.New(cfg)
```

### Step 3: Inject the Database (The "Plug-and-Play" step)

Now provide your SQL connection to the app. When you do this, Mana immediately detects it and routes all its core features to use standard SQL instead of local JSON files.

```go
// manadb.Postgres can also be manadb.MySQL or manadb.SQLite
app.WithDatabase(dbConn, manadb.Postgres)
```

### Step 4: Shared Transactions (The Magic)

Because Mana is using the same `*sql.DB` connection pool as your own application code, you can group your own table updates and Mana's framework updates into a single **Atomic Transaction**.

If your server crashes halfway through, both operations roll back. Neither system gets out of sync!

```go
ctx := context.Background()

// 1. Begin a transaction as you normally would for your own app
tx, err := dbConn.BeginTx(ctx, nil)

// 2. Perform a custom UPDATE in your own application's table
_, err = tx.ExecContext(ctx, "UPDATE your_inventory SET status = 'sold' WHERE item_id = 1")
if err != nil {
    tx.Rollback()
    return
}

// 3. IMPORTANT: Wrap the transaction inside the Context!
txCtx := manadb.WithTx(ctx, tx)

// 4. Pass the modified txCtx into any Mana function.
// Mana detects the transaction from the Context and safely executes inside it!
_, err = app.MessageStore().SaveMessage(txCtx, core.Message{
    Type:    "system_alert",
    RoomID:  "global",
    Payload: []byte("Item sold!"),
}, []string{"buyer_id"})

if err != nil {
    // If Mana fails to save the message, YOUR inventory update is safely rolled back too!
    tx.Rollback()
    return
}

// 5. Commit the transaction.
// The inventory state and the chat message are saved to the physical disk at the exact same millisecond. 
tx.Commit()
```

## Running the example

You can test the full code provided in the `main.go` file inside this `examples/custom_db/` folder. Just update your DSN string to point to your development database!
