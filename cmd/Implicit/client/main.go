package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"

	"github.com/labstack/echo/v5"

	"OAuth2/cmd/Implicit/types"
)

var (
	// stateMap 存储已生成的 state 值，用于 CSRF 防护
	stateMap = map[string]bool{}
	stateMu  sync.RWMutex
	// accessToken 存储获取到的访问令牌
	accessToken string
)

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	state := hex.EncodeToString(b)

	stateMu.Lock()
	stateMap[state] = true
	stateMu.Unlock()

	return state, nil
}

func consumeState(state string) bool {
	stateMu.RLock()
	_, ok := stateMap[state]
	stateMu.RUnlock()
	return ok
}

func main() {
	e := echo.New()

	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Implicit", "views"))
	e.Renderer = types.NewTemplateRenderer(
		filepath.Join(viewsDir, "home.html"),
		filepath.Join(viewsDir, "callback.html"),
		filepath.Join(viewsDir, "resource.html"),
		filepath.Join(viewsDir, "debug.html"),
		filepath.Join(viewsDir, "no_token.html"),
	)

	e.GET("/", handleHome)
	e.GET("/login", handleLogin)
	e.GET("/callback", handleCallback)
	e.POST("/token-store", handleTokenStore)
	e.GET("/resource", handleResource)
	e.GET("/debug", handleDebug)

	if err := e.Start(":8080"); err != nil {
		slog.Error("failed to start client application", "error", err)
	}
}

func handleHome(c *echo.Context) error {
	return c.Render(http.StatusOK, "home.html", nil)
}

// handleLogin 将用户重定向到授权服务器（RFC 4.2.1）
func handleLogin(c *echo.Context) error {
	state, err := generateState()
	if err != nil {
		return c.String(http.StatusInternalServerError, "internal server error")
	}

	authURL := fmt.Sprintf(
		"http://localhost:8081/authorize?response_type=token&client_id=%s&redirect_uri=%s&state=%s",
		"implicit-client-1",
		url.QueryEscape("http://localhost:8080/callback"),
		state,
	)

	return c.Redirect(http.StatusFound, authURL)
}

// handleCallback 返回包含 JS 的 HTML 页面，从 fragment 提取 access token
func handleCallback(c *echo.Context) error {
	state := c.QueryParam("state")
	if state != "" && !consumeState(state) {
		return c.String(http.StatusBadRequest, "invalid state parameter, possible CSRF attack")
	}

	return c.Render(http.StatusOK, "callback.html", nil)
}

// handleTokenStore 接收来自浏览器 JS 提交的 access token
func handleTokenStore(c *echo.Context) error {
	accessToken = c.FormValue("access_token")
	if accessToken == "" {
		return c.String(http.StatusBadRequest, "missing access_token")
	}
	return c.String(http.StatusOK, "token stored")
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

func handleDebug(c *echo.Context) error {
	tokenStatus := "not obtained"
	if accessToken != "" {
		tokenStatus = "obtained (value hidden)"
	}

	return c.Render(http.StatusOK, "debug.html", map[string]string{
		"TokenStatus": tokenStatus,
	})
}
