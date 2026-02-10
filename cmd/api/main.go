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

	defaultProviderType := ai.ProviderOpenRouter
	if cfg.AIProvider != "" {
		if pt, err := ai.GetProviderFromString(cfg.AIProvider); err == nil {
			defaultProviderType = pt
		}
	}

	if cfg.GeminiAPIKey != "" {
		p, _ := ai.NewProvider(ctx, ai.ProviderConfig{
			Type:   ai.ProviderGemini,
			APIKey: cfg.GeminiAPIKey,
			Models: cfg.GeminiModels,
		})
		if p != nil {
			providers[ai.ProviderGemini] = p
			log.Printf("Initialized AI provider: gemini")
		}
	}

	if cfg.OpenRouterAPIKey != "" {
		p, _ := ai.NewProvider(ctx, ai.ProviderConfig{
			Type:   ai.ProviderOpenRouter,
			APIKey: cfg.OpenRouterAPIKey,
			Models: cfg.OpenRouterModels,
		})
		if p != nil {
			providers[ai.ProviderOpenRouter] = p
			log.Printf("Initialized AI provider: openrouter")
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
	notionHandler := handlers.NewNotionHandler(cfg, userRepo)
	obsidianHandler := handlers.NewObsidianHandler(cfg, userRepo)
	discoveryHandler := handlers.NewDiscoveryHandler()
	userHandler := handlers.NewUserHandler(cfg, userRepo)
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

	// Serve static assets (internal UI)
	router.Static("/assets", "./assets")

	// root health check (public - no auth required)
	router.GET("/health", healthHandler.Handle)

	// API routes
	v1 := router.Group("/api/v1")
	{
		// Auth routes (public - no auth required for OAuth flow)
		auth := v1.Group("/auth")
		{
			auth.GET("/notion", authHandler.NotionLogin)
			auth.GET("/notion/callback", authHandler.NotionCallback)
			auth.GET("/obsidian", authHandler.ObsidianLogin)
		}

		// Public health check
		v1.GET("/health", healthHandler.Handle)

		// Protected routes - require API key authentication
		protected := v1.Group("")
		protected.Use(middleware.APIKeyAuth())
		{
			// Info routes
			protected.GET("/info/providers", discoveryHandler.GetSupportedNoteProviders)

			// User & Provider Management routes (require both API key and user auth)
			user := protected.Group("/user")
			user.Use(middleware.UserAuth())
			{
				user.GET("/status", userHandler.GetStatus)
				user.POST("/settings", userHandler.UpdateSettings)
				user.POST("/disconnect", userHandler.DisconnectProvider)

				user.POST("/databases", userHandler.CreateDatabase)

				// Notion Specific
				notionGroup := user.Group("/notion")
				{
					notionGroup.GET("/databases", notionHandler.ListDatabases)
					notionGroup.POST("/databases", userHandler.CreateDatabase) // Point to unified handler
					notionGroup.POST("/databases/rename", notionHandler.RenameDatabase)
					notionGroup.POST("/databases/remove", notionHandler.RemoveDatabase)
					notionGroup.POST("/databases/archive", notionHandler.RemoveDatabase)
					notionGroup.POST("/sync", notionHandler.SyncWord)
				}

				// Obsidian Specific
				obsidianGroup := user.Group("/obsidian")
				{
					obsidianGroup.GET("/status", obsidianHandler.GetStatus)
					obsidianGroup.POST("/settings", obsidianHandler.UpdateSettings)
					obsidianGroup.POST("/databases", userHandler.CreateDatabase) // Point to unified handler
					obsidianGroup.POST("/databases/remove", obsidianHandler.RemoveDatabase)
					obsidianGroup.POST("/sync", obsidianHandler.SyncWord)
				}
			}

			// Capture routes (require both API key and user auth)
			protected.POST("/capture", middleware.UserAuth(), captureHandler.Handle)

			// Compatibility routes for extension (require both API key and user auth)
			compat := protected.Group("")
			compat.Use(middleware.UserAuth())
			{
				compat.GET("/search", userHandler.ListDatabases)
				compat.POST("/databases", userHandler.CreateDatabase)
				compat.POST("/databases/*id", userHandler.ProxyQuery) // Wildcard handles both Notion IDs and Obsidian paths
				compat.POST("/pages", userHandler.ProxyCreate)
			}
		}
	}

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
