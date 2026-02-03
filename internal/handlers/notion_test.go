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

var _ = Describe("NotionHandler", func() {
	var (
		notionHandler *handlers.NotionHandler
		userRepo      repository.Repository
		cfg           *config.Config
		router        *gin.Engine
		recorder      *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		cfg = &config.Config{
			EncryptionKey: "6368616e676520746869732070617373776f726420746f206120736563726574", // 32-byte hex
		}
		userRepo = repository.NewMemoryUserRepository()
		notionHandler = handlers.NewNotionHandler(cfg, userRepo)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// Routes
		router.GET("/status", notionHandler.GetStatus)
		router.POST("/settings", notionHandler.UpdateSettings)
	})

	Describe("GetStatus", func() {
		Context("when user_id is missing", func() {
			It("should return connected: false", func() {
				req, _ := http.NewRequest("GET", "/status", nil)
				router.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				json.Unmarshal(recorder.Body.Bytes(), &resp)
				Expect(resp["connected"]).To(BeFalse())
			})
		})

		Context("when user is connected to Notion", func() {
			It("should return user details and connected: true", func() {
				userID := "user-123"
				userRepo.CreateOrUpdateUser(context.Background(), &models.User{
					ID: userID,
					Notes: &models.NotesConfig{
						Notion: &models.NotionConfig{
							AccessToken:   "encrypted-token",
							WorkspaceName: "Test Workspace",
						},
					},
				})

				req, _ := http.NewRequest("GET", "/status?user_id="+userID, nil)
				router.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				json.Unmarshal(recorder.Body.Bytes(), &resp)
				Expect(resp["connected"]).To(BeTrue())
				Expect(resp["workspace_name"]).To(Equal("Test Workspace"))
			})
		})
	})

	Describe("UpdateSettings", func() {
		It("should update database name and trigger re-discovery", func() {
			userID := "user-456"
			userRepo.CreateOrUpdateUser(context.Background(), &models.User{
				ID: userID,
				Notes: &models.NotesConfig{
					Notion: &models.NotionConfig{
						DefaultDatabaseID:   "old-id",
						DefaultDatabaseName: "Old Name",
					},
				},
			})

			body := `{"user_id": "user-456", "database_name": "New Name"}`
			req, _ := http.NewRequest("POST", "/settings", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))

			user, _ := userRepo.GetUser(context.Background(), userID)
			Expect(user.Notes.Notion.DefaultDatabaseName).To(Equal("New Name"))
			Expect(user.Notes.Notion.DefaultDatabaseID).To(BeEmpty()) // Verify re-discovery logic
		})
	})
})
