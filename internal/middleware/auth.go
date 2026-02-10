package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// APIKeyAuth validates the API key from the request header
// This ensures only your Chrome extension can call the API
func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")

		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "API key is required",
				"message": "Please provide a valid API key in the X-API-Key header",
			})
			c.Abort()
			return
		}

		// Get the expected API key from environment
		expectedKey := os.Getenv("API_KEY")
		if expectedKey == "" {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Server configuration error",
			})
			c.Abort()
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(expectedKey)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid API key",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// UserAuth validates the user token from the request header
// This identifies which user is making the request
func UserAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Authorization required",
				"message": "Please provide a valid authorization token",
			})
			c.Abort()
			return
		}

		// Extract Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Invalid authorization format",
				"message": "Authorization header must be in format: Bearer <token>",
			})
			c.Abort()
			return
		}

		token := parts[1]
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Empty token",
			})
			c.Abort()
			return
		}

		// For now, we'll use the token as the user_id
		// In a production system, you'd validate the token and extract user info
		// TODO: Implement proper JWT validation or session management
		c.Set("user_id", token)
		c.Next()
	}
}

// OptionalUserAuth is like UserAuth but doesn't require authentication
// Useful for endpoints that work better with auth but don't require it
func OptionalUserAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				token := parts[1]
				if token != "" {
					c.Set("user_id", token)
				}
			}
		}

		c.Next()
	}
}
