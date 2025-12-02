package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"pg_restore/config"

	"github.com/jackc/pgx/v5"
)

func Generation_Loop() {
	// Seed the random number generator
	rand.New(rand.NewSource(time.Now().UnixNano()))

	// Load Primary config
	// Assumes running from project root so it can find "Docker_Connections"
	primaryConfig, err := config.LoadDockerEnvConfig("Primary.env")
	if err != nil {
		log.Fatalf("Failed to load Primary config: %v", err)
	}

	log.Printf("Connecting to Primary at %s:%d...", primaryConfig.Host, primaryConfig.Port)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, primaryConfig.Dsn)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer conn.Close(ctx)

	log.Println("Connected. Starting data generation loop (every 1 second)...")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	counter := 1
	for range ticker.C {
		msg := fmt.Sprintf("Auto-generated entry %d", counter)
		val := rand.Float64() * 1000

		_, err := conn.Exec(ctx, "INSERT INTO test_data (counter, message, value) VALUES ($1, $2, $3)", counter, msg, val)
		if err != nil {
			log.Printf("Error inserting row %d: %v", counter, err)
		} else {
			fmt.Printf("Inserted row: %d | %s | %.2f\n", counter, msg, val)
		}

		counter++
	}
}
