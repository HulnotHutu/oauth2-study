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
	"time"

	"github.com/labstack/echo/v5"

	"OAuth2/cmd/Resource-Owner-Password-Credentials/types"
)

var (
	accessToken  string
	refreshToken string
	expiresAt    time.Time
)

func main() {
	e := echo.New()

	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Resource-Owner-Password-Credentials", "views"))
	e.Renderer = types.NewTemplateRenderer(
		filepath.Join(viewsDir, "home.html"),
		filepath.Join(viewsDir, "login.html"),
		filepath.Join(viewsDir, "success.html"),
		filepath.Join(viewsDir, "resource.html"),
		filepath.Join(viewsDir, "debug.html"),
		filepath.Join(viewsDir, "no_token.html"),
	)

	e.GET("/", handleHome)
	e.GET("/login", handleLoginForm)
	e.POST("/login", handleLogin)
	e.GET("/resource", handleResource)
	e.GET("/debug", handleDebug)

	if err := e.Start(":8080"); err != nil {
		slog.Error("failed to start client application", "error", err)
	}
}

func handleHome(c *echo.Context) error {
	return c.Render(http.StatusOK, "home.html", nil)
}

func handleLoginForm(c *echo.Context) error {
	return c.Render(http.StatusOK, "login.html", nil)
}

// handleLogin 使用用户名密码直接获取 access token（RFC 4.3.2）
func handleLogin(c *echo.Context) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		return c.String(http.StatusBadRequest, "username and password are required")
	}

	data := url.Values{
		"grant_type":    {"password"},
		"username":      {username},
		"password":      {password},
		"client_id":     {"ropc-client-1"},
		"client_secret": {"ropc-client-secret-1"},
	}

	resp, err := http.PostForm("http://localhost:8081/token", data)
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to request token: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to read response: %v", err))
	}

	if resp.StatusCode != http.StatusOK {
		return c.String(http.StatusUnauthorized, fmt.Sprintf("authentication failed (HTTP %d): %s", resp.StatusCode, string(body)))
	}

	var tokenResp types.AccessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to parse token response: %v", err))
	}

	if tokenResp.AccessToken == "" {
		return c.String(http.StatusInternalServerError, "token response missing access_token")
	}

	// 存储令牌（RFC 4.3.1: 获取 token 后必须丢弃用户凭据）
	accessToken = tokenResp.AccessToken
	refreshToken = tokenResp.RefreshToken
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return c.Render(http.StatusOK, "success.html", map[string]string{
		"Message": fmt.Sprintf("Access token obtained successfully. Token will expire in %d seconds.", tokenResp.ExpiresIn),
	})
}

// handleResource 使用访问令牌访问资源服务器
func handleResource(c *echo.Context) error {
	if accessToken == "" {
		return c.Render(http.StatusOK, "no_token.html", nil)
	}

	body, statusCode, err := callResource()
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to request resource: %v", err))
	}

	// Token 过期，尝试刷新
	if statusCode == http.StatusUnauthorized {
		if err := refreshAccessToken(); err != nil {
			return c.Render(http.StatusOK, "resource.html", map[string]any{
				"StatusCode": statusCode,
				"Body":       string(body),
			})
		}

		body, statusCode, err = callResource()
		if err != nil {
			return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to retry resource request: %v", err))
		}
	}

	var pretty bytes.Buffer
	json.Indent(&pretty, body, "", "  ")

	return c.Render(http.StatusOK, "resource.html", map[string]any{
		"StatusCode": statusCode,
		"Body":       pretty.String(),
	})
}

// callResource 向资源服务器发起请求
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

// refreshAccessToken 使用 refresh_token 获取新的访问令牌
func refreshAccessToken() error {
	if refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {"ropc-client-1"},
		"client_secret": {"ropc-client-secret-1"},
	}

	resp, err := http.PostForm("http://localhost:8081/token", data)
	if err != nil {
		return fmt.Errorf("failed to request token refresh: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp types.AccessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return fmt.Errorf("token response missing access_token")
	}

	accessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		refreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return nil
}

func handleDebug(c *echo.Context) error {
	tokenStatus := "not obtained"
	refreshStatus := "not obtained"
	if accessToken != "" {
		tokenStatus = "obtained (value hidden)"
	}
	if refreshToken != "" {
		refreshStatus = "obtained (value hidden)"
	}

	return c.Render(http.StatusOK, "debug.html", map[string]string{
		"TokenStatus":  tokenStatus,
		"RefreshToken": refreshStatus,
	})
}
