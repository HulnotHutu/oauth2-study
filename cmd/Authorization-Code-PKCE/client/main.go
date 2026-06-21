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
	"time"

	"github.com/labstack/echo/v5"

	"OAuth2/cmd/Authorization-Code/types"
)

var (
	// stateMap 存储已生成的 state 值，用于 CSRF 防护
	stateMap = map[string]bool{}
	stateMu  sync.RWMutex
	// accessToken 存储获取到的访问令牌
	accessToken string
	// refreshToken 存储刷新令牌
	refreshToken string
	// expiresAt 记录 access token 过期时间
	expiresAt time.Time
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

	// 设置模板渲染器
	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Authorization-Code", "views"))
	e.Renderer = types.NewTemplateRenderer(
		filepath.Join(viewsDir, "home.html"),
		filepath.Join(viewsDir, "callback_success.html"),
		filepath.Join(viewsDir, "resource.html"),
		filepath.Join(viewsDir, "debug.html"),
		filepath.Join(viewsDir, "no_token.html"),
	)

	e.GET("/", handleHome)
	e.GET("/login", handleLogin)
	e.GET("/callback", handleCallback)
	e.GET("/resource", handleResource)
	e.GET("/debug", handleDebug)

	if err := e.Start(":8080"); err != nil {
		slog.Error("failed to start client application", "error", err)
	}
}

func handleHome(c *echo.Context) error {
	return c.Render(http.StatusOK, "home.html", nil)
}

// handleLogin 将用户重定向到授权服务器
func handleLogin(c *echo.Context) error {
	state, err := generateState()
	if err != nil {
		return c.String(http.StatusInternalServerError, "internal server error")
	}

	authURL := fmt.Sprintf(
		"http://localhost:8081/authorize?response_type=code&client_id=%s&redirect_uri=%s&state=%s",
		"oauth-client-1",
		url.QueryEscape("http://localhost:8080/callback"),
		state,
	)

	return c.Redirect(http.StatusFound, authURL)
}

// handleCallback 处理授权服务器回调，用授权码换取访问令牌
func handleCallback(c *echo.Context) error {
	code := c.QueryParam("code")
	state := c.QueryParam("state")

	if code == "" {
		return c.String(http.StatusBadRequest, "missing authorization code")
	}
	if !consumeState(state) {
		return c.String(http.StatusBadRequest, "invalid state parameter, possible CSRF attack")
	}

	token, err := exchangeCode(code)
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to obtain token: %v", err))
	}

	accessToken = token

	return c.Render(http.StatusOK, "callback_success.html", nil)
}

// exchangeCode 用授权码向授权服务器换取访问令牌
func exchangeCode(code string) (string, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"client_id":     {"oauth-client-1"},
		"client_secret": {"oauth-client-secret-1"},
	}

	resp, err := http.PostForm("http://localhost:8081/token", data)
	if err != nil {
		return "", fmt.Errorf("failed to request token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp types.AccessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}

	// 存储 refresh token 和过期时间
	refreshToken = tokenResp.RefreshToken
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return tokenResp.AccessToken, nil
}

// refreshAccessToken 使用 refresh_token 获取新的访问令牌（RFC 6）
func refreshAccessToken() error {
	if refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {"oauth-client-1"},
		"client_secret": {"oauth-client-secret-1"},
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

	// 更新存储的令牌和过期时间
	accessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		refreshToken = tokenResp.RefreshToken // 轮换后的新 refresh token
	}
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return nil
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

	// 如果 access token 过期（401），尝试用 refresh token 刷新
	if statusCode == http.StatusUnauthorized {
		if err := refreshAccessToken(); err != nil {
			return c.Render(http.StatusOK, "resource.html", map[string]any{
				"StatusCode": statusCode,
				"Body":       string(body),
			})
		}

		// 使用新 token 重试
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

func handleDebug(c *echo.Context) error {
	tokenStatus := "not obtained"
	if accessToken != "" {
		tokenStatus = "obtained (value hidden)"
	}

	refreshStatus := "not obtained"
	if refreshToken != "" {
		refreshStatus = "obtained (value hidden)"
	}

	return c.Render(http.StatusOK, "debug.html", map[string]string{
		"TokenStatus":  tokenStatus,
		"RefreshToken": refreshStatus,
	})
}
