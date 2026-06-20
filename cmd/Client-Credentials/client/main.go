package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"OAuth2/cmd/Client-Credentials/types"
)

var (
	accessToken string
	expiresAt   time.Time
)

func main() {
	e := echo.New()

	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Client-Credentials", "views"))
	e.Renderer = types.NewTemplateRenderer(
		filepath.Join(viewsDir, "home.html"),
		filepath.Join(viewsDir, "success.html"),
		filepath.Join(viewsDir, "resource.html"),
		filepath.Join(viewsDir, "debug.html"),
		filepath.Join(viewsDir, "no_token.html"),
	)

	e.GET("/", handleHome)
	e.GET("/token", handleToken)
	e.GET("/resource", handleResource)
	e.GET("/debug", handleDebug)

	if err := e.Start(":8080"); err != nil {
		slog.Error("failed to start client application", "error", err)
	}
}

func handleHome(c *echo.Context) error {
	return c.Render(http.StatusOK, "home.html", nil)
}

// handleToken 直接通过客户端凭据获取 access token（RFC 4.4.2）
func handleToken(c *echo.Context) error {
	data := url.Values{
		"grant_type": {"client_credentials"},
	}

	req, err := http.NewRequest("POST", "http://localhost:8081/token", strings.NewReader(data.Encode()))
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to create request: %v", err))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("cc-client-1", "cc-client-secret-1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to request token: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to read response: %v", err))
	}

	if resp.StatusCode != http.StatusOK {
		return c.String(http.StatusUnauthorized, fmt.Sprintf("client authentication failed (HTTP %d): %s", resp.StatusCode, string(body)))
	}

	var tokenResp types.AccessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to parse token response: %v", err))
	}

	if tokenResp.AccessToken == "" {
		return c.String(http.StatusInternalServerError, "token response missing access_token")
	}

	accessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return c.Render(http.StatusOK, "success.html", map[string]string{
		"Message": fmt.Sprintf("Client credentials token obtained successfully (expires in %d seconds, no refresh token available).", tokenResp.ExpiresIn),
	})
}

// handleResource 使用访问令牌访问资源服务器
func handleResource(c *echo.Context) error {
	if accessToken == "" {
		return c.Render(http.StatusOK, "no_token.html", nil)
	}

	// 检查是否过期（Client Credentials 模式无 refresh token，过期只能重新获取）
	if time.Now().After(expiresAt) {
		accessToken = ""
		return c.Render(http.StatusOK, "resource.html", map[string]any{
			"StatusCode": http.StatusUnauthorized,
			"Body":       "{\n  \"error\": \"token_expired\",\n  \"error_description\": \"access token has expired, please obtain a new one\"\n}",
		})
	}

	body, statusCode, err := callResource()
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to request resource: %v", err))
	}

	var pretty bytes.Buffer
	json.Indent(&pretty, body, "", "  ")

	return c.Render(http.StatusOK, "resource.html", map[string]any{
		"StatusCode": statusCode,
		"Body":       pretty.String(),
	})
}

func callResource() ([]byte, int, error) {
	req, err := http.NewRequest("GET", "http://localhost:8082/resource", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to request resource server: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}

	return body, resp.StatusCode, nil
}

func handleDebug(c *echo.Context) error {
	tokenStatus := "not obtained"
	if accessToken != "" {
		tokenStatus = "obtained (value hidden)"
	}

	return c.Render(http.StatusOK, "debug.html", map[string]string{
		"TokenStatus": tokenStatus,
		"ClientID":    "cc-client-1",
	})
}
