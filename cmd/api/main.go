package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/geekabo93/lingofetch/internal/ai"
	"github.com/geekabo93/lingofetch/internal/config"
	"github.com/geekabo93/lingofetch/internal/handlers"
	"github.com/geekabo93/lingofetch/internal/middleware"
	"github.com/geekabo93/lingofetch/internal/repository"
	"github.com/geekabo93/lingofetch/internal/sync/notion"
	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting LingoFetch API server on port %s", cfg.Port)
	log.Printf("Environment: %s", cfg.Environment)

	// Initialize AI providers
	ctx := context.Background()
	providers := make(map[ai.ProviderType]ai.Provider)

	defaultProviderType := ai.ProviderLlama
	if cfg.AIProvider != "" {
		if pt, err := ai.GetProviderFromString(cfg.AIProvider); err == nil {
			defaultProviderType = pt
		}
	}

	if cfg.GeminiAPIKey != "" {
		p, _ := ai.NewProvider(ctx, ai.ProviderConfig{Type: ai.ProviderGemini, APIKey: cfg.GeminiAPIKey})
		if p != nil {
			providers[ai.ProviderGemini] = p
			log.Printf("Initialized AI provider: gemini")
		}
	}

	if cfg.LlamaAPIKey != "" {
		p, _ := ai.NewProvider(ctx, ai.ProviderConfig{Type: ai.ProviderLlama, APIKey: cfg.LlamaAPIKey})
		if p != nil {
			providers[ai.ProviderLlama] = p
			log.Printf("Initialized AI provider: llama")
		}
	}

	if len(providers) == 0 {
		log.Fatalf("No AI providers configured")
	}

	defer func() {
		for _, p := range providers {
			p.Close()
		}
	}()

	// Initialize User Repository (Firestore with Memory fallback)
	var userRepo repository.Repository
	fRepo, err := repository.NewUserRepository(ctx, cfg.GCPProjectID, cfg.FirestoreDatabaseID)
	if err != nil {
		log.Printf("Warning: Could not initialize Firestore: %v", err)
		log.Printf("👉 To use Firestore, ensure:")
		log.Printf("   1. GCP_PROJECT_ID is set in your .env")
		log.Printf("   2. You have created a Firestore database in Native Mode at https://console.cloud.google.com/firestore")
		log.Printf("   3. Your credentials file exists and is correctly referenced")
		log.Printf("Falling back to in-memory mode for local testing (data will be lost on restart).")
		userRepo = repository.NewMemoryUserRepository()
	} else {
		userRepo = fRepo
		defer userRepo.Close()
		log.Printf("Initialized Firestore repository")
	}

	// Initialize Notion adapter (Global Fallback)
	notionAdapter := notion.NewAdapter(cfg.NotionAPIKey, cfg.NotionDatabaseID)
	log.Printf("Initialized global note provider: %s", notionAdapter.Name())

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(cfg, userRepo)
	captureHandler := handlers.NewCaptureHandler(cfg, providers, defaultProviderType, userRepo, notionAdapter)
	healthHandler := handlers.NewHealthHandler()

	// Setup Gin router
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(c *gin.Context) {
		log.Printf("[Incoming] %s %s from %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())
		c.Next()
	})
	router.Use(middleware.Logger()) // Use custom logger
	router.Use(middleware.CORS())

	// Serve static assets (images, icons, etc)
	router.Static("/assets", "./extension/public")

	// API routes
	v1 := router.Group("/api/v1")
	{
		// Auth routes
		auth := v1.Group("/auth")
		{
			auth.GET("/notion", authHandler.NotionLogin)
			auth.GET("/notion/callback", authHandler.NotionCallback)
		}

		// Capture routes
		v1.POST("/capture", captureHandler.Handle)
		v1.GET("/user/status", captureHandler.GetStatus)
		v1.POST("/user/settings", captureHandler.UpdateSettings)
		v1.GET("/user/notion/databases", captureHandler.ListDatabases)
		v1.POST("/user/notion/databases", captureHandler.CreateLanguageDatabase)
		v1.POST("/user/notion/databases/rename", captureHandler.RenameDatabase)
		v1.POST("/user/notion/sync", captureHandler.SyncWord)
		v1.POST("/user/disconnect", captureHandler.DisconnectNotion)
		v1.GET("/health", healthHandler.Handle)
	}

	// Root health check
	router.GET("/health", healthHandler.Handle)

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      router,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}
