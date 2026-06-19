package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
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
	Scope       string
	ExpiresAt   time.Time
}

// AccessToken 访问令牌
type AccessToken struct {
	Token     string
	ClientID  string
	Username  string
	Scope     string
	ExpiresAt time.Time
}

// codeRecord 追踪已消费的授权码及签发的令牌（用于重用检测和令牌撤销）
type codeRecord struct {
	tokenIDs []string
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

	usedCodes    = map[string]codeRecord{}
	usedCodesMu  sync.RWMutex

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

// authenticateClient 从请求中提取并验证客户端凭据
// 优先使用 Authorization: Basic header，回退到 form 参数
func authenticateClient(c *echo.Context) (clientID, clientSecret string, ok bool) {
	auth := c.Request().Header.Get("Authorization")
	if payloadBase64, ok := strings.CutPrefix(auth, "Basic "); ok {
		payload, err := base64.StdEncoding.DecodeString(payloadBase64)
		if err == nil {
			parts := strings.SplitN(string(payload), ":", 2)
			if len(parts) == 2 {
				return parts[0], parts[1], true
			}
		}
	}
	return c.FormValue("client_id"), c.FormValue("client_secret"), true
}

// errorRedirect 按照 RFC 4.1.2.1 规范构造错误重定向
func errorRedirect(c *echo.Context, redirectURI, state string, code types.ErrorCode, desc string) error {
	loc := fmt.Sprintf("%s?error=%s&error_description=%s", redirectURI,
		url.QueryEscape(string(code)), url.QueryEscape(desc))
	if state != "" {
		loc += "&state=" + url.QueryEscape(state)
	}
	return c.Redirect(http.StatusFound, loc)
}

// revokeTokens 撤销指定列表中的所有令牌
func revokeTokens(tokenIDs []string) {
	accessTokensMu.Lock()
	defer accessTokensMu.Unlock()
	for _, tid := range tokenIDs {
		delete(accessTokens, tid)
	}
}

func main() {
	e := echo.New()

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

// handleAuthorize 展示用户登录和授权页面（RFC 4.1.1）
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
	if responseType != "code" {
		return errorRedirect(c, redirectURI, state, types.ErrorUnsupportedResponseType,
			"response_type must be 'code'")
	}

	return c.Render(http.StatusOK, "authorize.html", map[string]string{
		"ClientID":     clientID,
		"RedirectURI":  redirectURI,
		"ResponseType": responseType,
		"State":        state,
	})
}

// handleLogin 处理用户登录和授权（RFC 4.1.2 / 4.1.2.1）
func handleLogin(c *echo.Context) error {
	clientID := c.FormValue("client_id")
	redirectURI := c.FormValue("redirect_uri")
	state := c.FormValue("state")
	approve := c.FormValue("approve")
	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		return errorRedirect(c, redirectURI, state, types.ErrorInvalidRequest,
			"username and password are required")
	}
	if approve != "yes" {
		return errorRedirect(c, redirectURI, state, types.ErrorAccessDenied,
			"resource owner denied the request")
	}

	code, err := generateRandomString(16)
	if err != nil {
		return errorRedirect(c, redirectURI, state, types.ErrorServerError,
			"internal server error")
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

// handleToken 用授权码换取访问令牌（RFC 4.1.3 / 4.1.4）
func handleToken(c *echo.Context) error {
	// 使用标准 response header（RFC 4.1.4）
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Pragma", "no-cache")

	grantType := c.FormValue("grant_type")
	code := c.FormValue("code")
	redirectURI := c.FormValue("redirect_uri")

	// 客户端认证（RFC 3.2.1）：支持 Basic Auth 和 body 参数两种方式
	clientID, clientSecret, _ := authenticateClient(c)

	if grantType != "authorization_code" {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{Error: types.ErrorUnsupportedGrantType})
	}

	client, ok := clients[clientID]
	if !ok || client.Secret != clientSecret {
		return c.JSON(http.StatusUnauthorized, types.ErrorResponse{Error: types.ErrorInvalidClient})
	}

	// 检查授权码重用（RFC 4.1.2：MUST deny reused code, SHOULD revoke tokens）
	usedCodesMu.RLock()
	used, wasUsed := usedCodes[code]
	usedCodesMu.RUnlock()

	if wasUsed {
		// 撤销此前基于该 code 签发的所有令牌
		revokeTokens(used.tokenIDs)
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "authorization code has already been used",
		})
	}

	authCodesMu.RLock()
	authCode, ok := authCodes[code]
	authCodesMu.RUnlock()

	if !ok {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "authorization code not found",
		})
	}
	if authCode.ClientID != clientID {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "authorization code was issued for a different client",
		})
	}
	if authCode.RedirectURI != redirectURI {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "redirect_uri mismatch",
		})
	}
	if time.Now().After(authCode.ExpiresAt) {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "authorization code has expired",
		})
	}

	username := authCode.Username
	scope := authCode.Scope

	// 从 authCodes 中删除（防止重复消费）
	authCodesMu.Lock()
	delete(authCodes, code)
	authCodesMu.Unlock()

	tokenValue, err := generateRandomString(32)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: types.ErrorServerError})
	}

	accessTokensMu.Lock()
	accessTokens[tokenValue] = AccessToken{
		Token:     tokenValue,
		ClientID:  clientID,
		Username:  username,
		Scope:     scope,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	accessTokensMu.Unlock()

	// 记录 code→token 映射，用于重用检测和令牌撤销
	usedCodesMu.Lock()
	usedCodes[code] = codeRecord{tokenIDs: []string{tokenValue}}
	usedCodesMu.Unlock()

	return c.JSON(http.StatusOK, types.AccessTokenResponse{
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
