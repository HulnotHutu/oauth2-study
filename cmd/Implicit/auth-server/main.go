package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"OAuth2/cmd/Implicit/types"
)

// Client 注册的客户端信息
type Client struct {
	ID           string
	RedirectURIs []string
}

// AccessToken 访问令牌
type AccessToken struct {
	Token     string
	ClientID  string
	Username  string
	Scope     string
	ExpiresAt time.Time
}

var (
	clients = map[string]Client{
		"implicit-client-1": {
			ID:           "implicit-client-1",
			RedirectURIs: []string{"http://localhost:8080/callback"},
		},
	}

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

// errorFragment 构造错误 fragment（RFC 4.2.2.1）
func errorFragment(redirectURI, state string, code types.ErrorCode, desc string) string {
	loc := fmt.Sprintf("%s#error=%s&error_description=%s", redirectURI,
		url.QueryEscape(string(code)), url.QueryEscape(desc))
	if state != "" {
		loc += "&state=" + url.QueryEscape(state)
	}
	return loc
}

func main() {
	e := echo.New()

	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Implicit", "views"))
	e.Renderer = types.NewTemplateRenderer(
		filepath.Join(viewsDir, "authorize.html"),
	)

	e.GET("/authorize", handleAuthorize)
	e.POST("/authorize", handleLogin)
	e.POST("/introspect", handleIntrospect)

	if err := e.Start(":8081"); err != nil {
		slog.Error("failed to start authorization server", "error", err)
	}
}

// handleAuthorize 展示用户登录和授权页面（RFC 4.2.1）
func handleAuthorize(c *echo.Context) error {
	clientID := c.QueryParam("client_id")
	redirectURI := c.QueryParam("redirect_uri")
	responseType := c.QueryParam("response_type")
	state := c.QueryParam("state")

	client, ok := clients[clientID]
	if !ok {
		return c.String(http.StatusBadRequest, fmt.Sprintf("unknown client: %s", clientID))
	}
	if redirectURI == "" {
		return c.String(http.StatusBadRequest, "missing redirect_uri")
	}
	if !slices.Contains(client.RedirectURIs, redirectURI) {
		return c.String(http.StatusBadRequest, "invalid redirect_uri")
	}
	if responseType != "token" {
		loc := errorFragment(redirectURI, state, types.ErrorUnsupportedResponseType,
			"response_type must be 'token'")
		return c.Redirect(http.StatusFound, loc)
	}

	return c.Render(http.StatusOK, "authorize.html", map[string]string{
		"ClientID":     clientID,
		"RedirectURI":  redirectURI,
		"ResponseType": responseType,
		"State":        state,
	})
}

// handleLogin 处理用户登录并签发 access token（RFC 4.2.2）
func handleLogin(c *echo.Context) error {
	clientID := c.FormValue("client_id")
	redirectURI := c.FormValue("redirect_uri")
	state := c.FormValue("state")
	approve := c.FormValue("approve")
	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		loc := errorFragment(redirectURI, state, types.ErrorInvalidRequest,
			"username and password are required")
		return c.Redirect(http.StatusFound, loc)
	}
	if approve != "yes" {
		loc := errorFragment(redirectURI, state, types.ErrorAccessDenied,
			"resource owner denied the request")
		return c.Redirect(http.StatusFound, loc)
	}

	tokenValue, err := generateRandomString(32)
	if err != nil {
		return c.String(http.StatusInternalServerError, "internal server error")
	}

	accessTokensMu.Lock()
	accessTokens[tokenValue] = AccessToken{
		Token:     tokenValue,
		ClientID:  clientID,
		Username:  username,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	accessTokensMu.Unlock()

	// 使用 302 重定向，access token 放在 URI fragment 中（RFC 4.2.2）
	location := fmt.Sprintf("%s#access_token=%s&token_type=%s&expires_in=%d&state=%s",
		redirectURI, tokenValue, "Bearer", 3600, url.QueryEscape(state))
	return c.Redirect(http.StatusFound, location)
}

// handleIntrospect 令牌 introspection 端点
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
