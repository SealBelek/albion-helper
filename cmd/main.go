package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"albion-helper/data"
	"albion-helper/internal/api"
	"albion-helper/internal/db"
	"albion-helper/internal/enricher"
	"albion-helper/internal/nats"
	"albion-helper/internal/service"
	"albion-helper/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	profile := flag.Bool("profile", false, "enable pprof HTTP server on localhost:6060")
	debug := flag.Bool("debug", false, "enable debug mode: write heap profiles every 30s and log memory usage")
	flag.Parse()

	loadEnv()

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
	data.Items = nil
	data.World = nil
	runtime.GC()

	db.StartCleanup(database, 5*time.Minute)

	if *profile {
		go func() {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}

	if *debug {
		go writeHeapProfiles()
	}

	subscriber := nats.NewSubscriber(database)
	go func() {
		if err := subscriber.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "NATS subscriber error: %v\n", err)
		}
	}()
	defer subscriber.Stop()

	apiClient := api.NewClient(os.Getenv("API_PROXY"))
	enricher.New(database, apiClient).Start()
	itemSvc := service.NewItemService(database)
	priceSvc := service.NewPriceService(database, apiClient)
	mmSvc := service.NewMarketMakerService(database)

	p := tea.NewProgram(tui.NewModel(itemSvc, priceSvc, mmSvc), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}

func loadEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		os.Setenv(strings.TrimSpace(k), strings.TrimSpace(v))
	}
}

func writeHeapProfiles() {
	os.MkdirAll("profiles", 0755)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		var m runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m)
		log.Printf("[debug] MemStats: Alloc=%s Sys=%s NumGC=%d HeapInuse=%s",
			formatBytes(m.Alloc), formatBytes(m.Sys), m.NumGC, formatBytes(m.HeapInuse))

		filename := fmt.Sprintf("profiles/memprofile_%d.pprof", time.Now().Unix())
		f, err := os.Create(filename)
		if err != nil {
			log.Printf("[debug] failed to create heap profile: %v", err)
			continue
		}
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Printf("[debug] failed to write heap profile: %v", err)
		}
		f.Close()

		oldFiles, _ := os.ReadDir("profiles")
		for i := 0; i < len(oldFiles)-10; i++ {
			os.Remove("profiles/" + oldFiles[i].Name())
		}
	}
}

func formatBytes(b uint64) string {
	const mb = 1024 * 1024
	if b >= mb {
		return fmt.Sprintf("%.1fMB", float64(b)/float64(mb))
	}
	const kb = 1024
	if b >= kb {
		return fmt.Sprintf("%.1fKB", float64(b)/float64(kb))
	}
	return fmt.Sprintf("%dB", b)
}