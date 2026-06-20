package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"

	"OAuth2/cmd/Resource-Owner-Password-Credentials/types"
)

func main() {
	e := echo.New()

	e.GET("/resource", handleResource)

	if err := e.Start(":8082"); err != nil {
		slog.Error("failed to start resource server", "error", err)
	}
}

func handleResource(c *echo.Context) error {
	authHeader := c.Request().Header.Get("Authorization")
	if authHeader == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error":             "missing_token",
			"error_description": "access token is required",
		})
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error":             "invalid_token",
			"error_description": "token must use Bearer scheme",
		})
	}

	token := parts[1]

	valid, username := validateToken(token)
	if !valid {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error":             "invalid_token",
			"error_description": "token is invalid or expired",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message": "protected resource accessed successfully",
		"data": map[string]string{
			"username": username,
			"user_id":  "user_003",
			"email":    "ropc@example.com",
			"note":     "this resource is protected by OAuth2 resource owner password credentials flow",
		},
	})
}

func validateToken(token string) (bool, string) {
	resp, err := http.PostForm("http://localhost:8081/introspect", url.Values{"token": {token}})
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()

	var result types.IntrospectResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, ""
	}

	return result.Active, result.Username
}
