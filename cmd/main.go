package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"albion-helper/data"
	"albion-helper/internal/db"
	"albion-helper/internal/nats"
	"albion-helper/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	profile := flag.Bool("profile", false, "enable pprof HTTP server on localhost:6060")
	flag.Parse()

	database, err := db.Open("db/items.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.SeedData(database, data.Items, data.World); err != nil {
		fmt.Fprintf(os.Stderr, "failed to seed database: %v\n", err)
		os.Exit(1)
	}

	db.StartCleanup(database, 5*time.Minute)

	if *profile {
		go func() {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}

	subscriber := nats.NewSubscriber(database)
	go func() {
		if err := subscriber.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "NATS subscriber error: %v\n", err)
		}
	}()
	defer subscriber.Stop()

	p := tea.NewProgram(tui.NewModel(database), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}