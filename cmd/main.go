package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"albion-helper/internal/db"
	"albion-helper/internal/nats"
	"albion-helper/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	database, err := db.Open("db/items.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	db.StartCleanup(database, 5*time.Minute)

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

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
