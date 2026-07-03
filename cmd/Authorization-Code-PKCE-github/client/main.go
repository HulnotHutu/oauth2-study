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

// logTransport 包装 http.RoundTripper 记录所有出站请求
type logTransport struct {
	rt http.RoundTripper
}

func (t *logTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	slog.Info("-> "+req.Method+" "+req.URL.String(),
		"headers", fmt.Sprintf("%v", req.Header),
	)
	resp, err := t.rt.RoundTrip(req)
	if err != nil {
		slog.Error("<- request failed", "error", err)
	} else {
		slog.Info("<- "+resp.Status, "status_code", resp.StatusCode)
	}
	return resp, err
}

type githubUser struct {
	Login         string `json:"login"`
	AvatarURL     string `json:"avatar_url"`
	Name          any    `json:"name"`
	PublicRepos   int    `json:"public_repos"`
	PublicGists   int    `json:"public_gists"`
	Followers     int    `json:"followers"`
	Following     int    `json:"following"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	Location      any    `json:"location"`
	Bio           any    `json:"bio"`
	TwitterHandle any    `json:"twitter_username"`
	HTMLURL       string `json:"html_url"`
}

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

func init() {
	// 挂载日志 transport 到默认 HTTP 客户端，覆盖所有出站请求
	http.DefaultClient.Transport = &logTransport{rt: http.DefaultTransport}
}

func main() {
	e := echo.New()

	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Authorization-Code-PKCE-github", "views"))
	e.Renderer = types.NewTemplateRenderer(
		filepath.Join(viewsDir, "home.html"),
		filepath.Join(viewsDir, "callback_success.html"),
		filepath.Join(viewsDir, "resource.html"),
		filepath.Join(viewsDir, "no_token.html"),
	)

	e.GET("/", handleHome)
	e.GET("/login", handleLogin)
	e.GET("/callback", handleCallback)
	e.GET("/resource", handleResource)

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
		"https://github.com/login/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&state=%s&code_challenge=%s&code_challenge_method=S256",
		"Ov23liF4n15u6R3x2KSd",
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
		"code":          {code},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"client_id":     {"Ov23liF4n15u6R3x2KSd"},
		"client_secret": {"cc4a36938141aa1a90820e205ca951394184e04e"},
		"code_verifier": {codeVerifier}, // PKCE
	}

	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
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
		"client_id":     {"Ov23liF4n15u6R3x2KSd"},
		"client_secret": {"cc4a36938141aa1a90820e205ca951394184e04e"},
	}

	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", bytes.NewBufferString(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
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
			return renderResource(c, statusCode, body)
		}

		body, statusCode, err = callResource()
		if err != nil {
			return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to retry resource request: %v", err))
		}
	}

	return renderResource(c, statusCode, body)
}

func renderResource(c *echo.Context, statusCode int, body []byte) error {
	var user githubUser
	if err := json.Unmarshal(body, &user); err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to parse user data: %v", err))
	}

	// ponytail: formatDate template func 不存在，直接格式化好再传
	joinDate, _ := time.Parse(time.RFC3339, user.CreatedAt)
	updateDate, _ := time.Parse(time.RFC3339, user.UpdatedAt)

	return c.Render(http.StatusOK, "resource.html", map[string]any{
		"StatusCode":  statusCode,
		"User":        user,
		"JoinedDate":  joinDate.Format("Jan 2, 2006"),
		"UpdatedDate": updateDate.Format("Jan 2, 2006"),
	})
}

func callResource() ([]byte, int, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
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
