package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

	"OAuth2/cmd/Authorization-Code-PKCE/types"
)

var (
	stateMap = map[string]bool{}
	stateMu  sync.RWMutex
	// codeVerifier 存储当前 PKCE 的 verifier（用于令牌交换时验证）
	codeVerifier string
	accessToken  string
	refreshToken string
	expiresAt    time.Time
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

// generateCodeVerifier 生成 PKCE code_verifier（RFC 7636 Section 4.1）
// 32 字节随机数 → base64url 编码 → 43 字符（符合 43-128 字符要求）
func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// computeCodeChallenge 计算 PKCE code_challenge（S256 方法）
// code_challenge = BASE64URL-ENCODE(SHA256(ASCII(code_verifier)))
func computeCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func main() {
	e := echo.New()

	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Authorization-Code-PKCE", "views"))
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

// handleLogin 生成 PKCE 参数并发起授权请求
func handleLogin(c *echo.Context) error {
	state, err := generateState()
	if err != nil {
		return c.String(http.StatusInternalServerError, "internal server error")
	}

	// 生成 PKCE code_verifier 和 code_challenge（S256）
	verifier, err := generateCodeVerifier()
	if err != nil {
		return c.String(http.StatusInternalServerError, "internal server error")
	}
	codeVerifier = verifier // 存储用于后续令牌交换
	challenge := computeCodeChallenge(verifier)

	authURL := fmt.Sprintf(
		"http://localhost:8081/authorize?response_type=code&client_id=%s&redirect_uri=%s&state=%s&code_challenge=%s&code_challenge_method=S256",
		"oauth-client-1",
		url.QueryEscape("http://localhost:8080/callback"),
		state,
		challenge,
	)

	return c.Redirect(http.StatusFound, authURL)
}

// handleCallback 处理授权服务器回调，用授权码 + code_verifier 换取访问令牌
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

// exchangeCode 用授权码 + PKCE code_verifier 向授权服务器换取访问令牌
func exchangeCode(code string) (string, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"client_id":     {"oauth-client-1"},
		"client_secret": {"oauth-client-secret-1"},
		"code_verifier": {codeVerifier}, // PKCE
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

	refreshToken = tokenResp.RefreshToken
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	// 交换完成后清空 code_verifier（一次性使用）
	codeVerifier = ""

	return tokenResp.AccessToken, nil
}

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

	accessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		refreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return nil
}

func handleResource(c *echo.Context) error {
	if accessToken == "" {
		return c.Render(http.StatusOK, "no_token.html", nil)
	}

	body, statusCode, err := callResource()
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to request resource: %v", err))
	}

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
