package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"wishlist-tracker/internal/api"
	"wishlist-tracker/internal/config"
	"wishlist-tracker/internal/db"
	"wishlist-tracker/internal/notify"
	"wishlist-tracker/internal/scheduler"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Wishlist Price Tracker starting...")

	// Load .env file if it exists (won't overwrite existing env vars)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found — using environment variables")
	} else {
		log.Println("Loaded .env file")
	}

	// Load config
	cfg := config.Load()

	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize database
	database, err := db.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()
	log.Println("Database initialized")

	// Initialize email sender
	emailer := notify.NewEmailer(cfg.SMTP)
	if cfg.SMTP.Username == "" {
		log.Println("⚠️  SMTP not configured — emails will be skipped. Set SMTP_USERNAME, SMTP_PASSWORD, SMTP_FROM env vars.")
	} else {
		log.Printf("Email configured: %s via %s:%d", cfg.SMTP.From, cfg.SMTP.Host, cfg.SMTP.Port)
	}

	// Initialize API
	handler := api.NewHandler(database, emailer)
	router := gin.Default()
	handler.RegisterRoutes(router)

	// Serve frontend static files
	router.StaticFile("/", "./web/index.html")
	router.StaticFile("/index.html", "./web/index.html")
	router.Static("/assets", "./web/assets")
	router.Static("/static", "./web/static")

	// Initialize and start scheduler
	poller := scheduler.NewPoller(database, emailer)
	if err := poller.Start(cfg.Scheduler.Cron); err != nil {
		log.Fatalf("Failed to start scheduler: %v", err)
	}
	defer poller.Stop()
	log.Printf("Scheduler started (cron: %s)", cfg.Scheduler.Cron)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Server listening on %s", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down...")
}
