package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"OAuth2/cmd/Authorization-Code/types"
)

// Client 注册的客户端信息
type Client struct {
	ID           string
	Secret       string
	RedirectURIs []string
}

// AuthorizationCode 一次性授权码
type AuthorizationCode struct {
	Code        string
	ClientID    string
	RedirectURI string
	Username    string
	ExpiresAt   time.Time
}

// AccessToken 访问令牌
type AccessToken struct {
	Token     string
	ClientID  string
	Username  string
	ExpiresAt time.Time
}

var (
	clients = map[string]Client{
		"oauth-client-1": {
			ID:     "oauth-client-1",
			Secret: "oauth-client-secret-1",
			RedirectURIs: []string{"http://localhost:8080/callback"},
		},
	}

	authCodes    = map[string]AuthorizationCode{}
	authCodesMu  sync.RWMutex

	accessTokens   = map[string]AccessToken{}
	accessTokensMu sync.RWMutex
)

func generateRandomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func main() {
	e := echo.New()

	// 设置模板渲染器
	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Authorization-Code", "views"))
	e.Renderer = types.NewTemplateRenderer(
		filepath.Join(viewsDir, "authorize.html"),
		filepath.Join(viewsDir, "client_info.html"),
	)

	e.GET("/authorize", handleAuthorize)
	e.POST("/authorize", handleLogin)
	e.POST("/token", handleToken)
	e.POST("/introspect", handleIntrospect)
	e.GET("/client", handleClientInfo)

	if err := e.Start(":8081"); err != nil {
		slog.Error("failed to start authorization server", "error", err)
	}
}

// handleAuthorize 展示用户登录和授权页面
func handleAuthorize(c *echo.Context) error {
	clientID := c.QueryParam("client_id")
	redirectURI := c.QueryParam("redirect_uri")
	responseType := c.QueryParam("response_type")
	state := c.QueryParam("state")

	client, ok := clients[clientID]
	if !ok {
		return c.String(http.StatusBadRequest, fmt.Sprintf("unknown client: %s", clientID))
	}

	validRedirect := slices.Contains(client.RedirectURIs, redirectURI)
	if !validRedirect {
		return c.String(http.StatusBadRequest, "invalid redirect_uri")
	}
	if responseType != "code" {
		return c.String(http.StatusBadRequest, "invalid response_type, must be 'code'")
	}

	return c.Render(http.StatusOK, "authorize.html", map[string]string{
		"ClientID":     clientID,
		"RedirectURI":  redirectURI,
		"ResponseType": responseType,
		"State":        state,
	})
}

// handleLogin 处理用户登录和授权
func handleLogin(c *echo.Context) error {
	clientID := c.FormValue("client_id")
	redirectURI := c.FormValue("redirect_uri")
	state := c.FormValue("state")
	approve := c.FormValue("approve")
	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		return c.String(http.StatusBadRequest, "username and password are required")
	}
	if approve != "yes" {
		return c.String(http.StatusForbidden, "authorization denied")
	}

	code, err := generateRandomString(16)
	if err != nil {
		return c.String(http.StatusInternalServerError, "internal server error")
	}

	authCodesMu.Lock()
	authCodes[code] = AuthorizationCode{
		Code:        code,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		Username:    username,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}
	authCodesMu.Unlock()

	location := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, code, state)
	return c.Redirect(http.StatusFound, location)
}

// handleToken 用授权码换取访问令牌
func handleToken(c *echo.Context) error {
	grantType := c.FormValue("grant_type")
	code := c.FormValue("code")
	redirectURI := c.FormValue("redirect_uri")
	clientID := c.FormValue("client_id")
	clientSecret := c.FormValue("client_secret")

	if grantType != "authorization_code" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}

	client, ok := clients[clientID]
	if !ok || client.Secret != clientSecret {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
	}

	authCodesMu.RLock()
	authCode, ok := authCodes[code]
	authCodesMu.RUnlock()

	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "authorization code not found"})
	}
	if authCode.ClientID != clientID {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "authorization code was issued for a different client"})
	}
	if authCode.RedirectURI != redirectURI {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "redirect_uri mismatch"})
	}
	if time.Now().After(authCode.ExpiresAt) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "authorization code has expired"})
	}

	// 从授权码记录中获取用户名
	username := authCode.Username

	// 一次性使用，删除授权码
	authCodesMu.Lock()
	delete(authCodes, code)
	authCodesMu.Unlock()

	tokenValue, err := generateRandomString(32)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "server_error"})
	}

	accessTokensMu.Lock()
	accessTokens[tokenValue] = AccessToken{
		Token:     tokenValue,
		ClientID:  clientID,
		Username:  username,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	accessTokensMu.Unlock()

	return c.JSON(http.StatusOK, types.TokenResponse{
		AccessToken: tokenValue,
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})
}

// handleIntrospect 令牌 introspection 端点，供资源服务器验证令牌
func handleIntrospect(c *echo.Context) error {
	token := c.FormValue("token")

	accessTokensMu.RLock()
	t, ok := accessTokens[token]
	accessTokensMu.RUnlock()

	if !ok || time.Now().After(t.ExpiresAt) {
		return c.JSON(http.StatusOK, types.IntrospectResponse{Active: false})
	}

	return c.JSON(http.StatusOK, types.IntrospectResponse{
		Active:   true,
		ClientID: t.ClientID,
		Username: t.Username,
		Exp:      t.ExpiresAt.Unix(),
	})
}

// handleClientInfo 展示已注册客户端信息
func handleClientInfo(c *echo.Context) error {
	return c.Render(http.StatusOK, "client_info.html", nil)
}
