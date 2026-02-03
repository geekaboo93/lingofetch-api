package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/geekabo93/lingofetch/internal/config"
	"github.com/geekabo93/lingofetch/internal/handlers"
	"github.com/geekabo93/lingofetch/internal/models"
	"github.com/geekabo93/lingofetch/internal/repository"
	"github.com/gin-gonic/gin"
)

var _ = Describe("ObsidianHandler", func() {
	var (
		obsidianHandler *handlers.ObsidianHandler
		userRepo        repository.Repository
		cfg             *config.Config
		router          *gin.Engine
		recorder        *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		cfg = &config.Config{
			EncryptionKey: "6368616e676520746869732070617373776f726420746f206120736563726574", // 32-byte hex
		}
		userRepo = repository.NewMemoryUserRepository()
		obsidianHandler = handlers.NewObsidianHandler(cfg, userRepo)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// Routes
		router.GET("/status", obsidianHandler.GetStatus)
		router.POST("/settings", obsidianHandler.UpdateSettings)
	})

	Describe("GetStatus", func() {
		Context("when success=true parameter is present", func() {
			It("should return the branded success HTML", func() {
				userID := "user-obs"
				userRepo.CreateOrUpdateUser(context.Background(), &models.User{
					ID: userID,
					Notes: &models.NotesConfig{
						Obsidian: &models.ObsidianConfig{
							AccessToken: "some-token",
						},
					},
				})

				req, _ := http.NewRequest("GET", "/status?user_id="+userID+"&success=true", nil)
				router.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
				Expect(recorder.Header().Get("Content-Type")).To(ContainSubstring("text/html"))
				Expect(recorder.Body.String()).To(ContainSubstring("Connection Successful"))
			})
		})

		Context("when normal status request", func() {
			It("should return Obsidian settings", func() {
				userID := "user-obs"
				userRepo.CreateOrUpdateUser(context.Background(), &models.User{
					ID: userID,
					Notes: &models.NotesConfig{
						Obsidian: &models.ObsidianConfig{
							BaseURL:     "http://localhost:27123",
							AccessToken: "token",
						},
					},
				})

				req, _ := http.NewRequest("GET", "/status?user_id="+userID, nil)
				router.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				json.Unmarshal(recorder.Body.Bytes(), &resp)
				Expect(resp["connected"]).To(BeTrue())
				Expect(resp["base_url"]).To(Equal("http://localhost:27123"))
			})
		})
	})

	Describe("UpdateSettings", func() {
		It("should encrypt access token and save settings", func() {
			userID := "user-obs-new"
			body := `{"user_id": "user-obs-new", "access_token": "my-secret-obsidian-token", "base_url": "http://127.0.0.1:27123"}`

			req, _ := http.NewRequest("POST", "/settings", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))

			user, _ := userRepo.GetUser(context.Background(), userID)
			Expect(user.Notes.Obsidian.BaseURL).To(Equal("http://127.0.0.1:27123"))
			Expect(user.Notes.Obsidian.DefaultDatabaseName).To(Equal(models.DefaultDatabaseName))
			Expect(user.Notes.Obsidian.AccessToken).ToNot(BeEmpty())
			Expect(user.Notes.Obsidian.AccessToken).ToNot(Equal("my-secret-obsidian-token")) // Should be encrypted
		})
	})
})
