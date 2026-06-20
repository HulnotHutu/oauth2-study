package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"OAuth2/cmd/Client-Credentials/types"
)

// Client 注册的客户端信息
type Client struct {
	ID     string
	Secret string
}

// AccessToken 访问令牌（客户端凭证模式没有用户概念）
type AccessToken struct {
	Token     string
	ClientID  string
	Scope     string
	ExpiresAt time.Time
}

var (
	clients = map[string]Client{
		"cc-client-1": {
			ID:     "cc-client-1",
			Secret: "cc-client-secret-1",
		},
		"cc-service-api": {
			ID:     "cc-service-api",
			Secret: "cc-api-secret-789",
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

// authenticateClient 从请求中提取并验证客户端凭据
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

func main() {
	e := echo.New()

	e.POST("/token", handleToken)
	e.POST("/introspect", handleIntrospect)

	if err := e.Start(":8081"); err != nil {
		slog.Error("failed to start authorization server", "error", err)
	}
}

// handleToken 处理令牌请求（RFC 4.4.2 / 4.4.3）
func handleToken(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Pragma", "no-cache")

	grantType := c.FormValue("grant_type")
	if grantType != "client_credentials" {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorUnsupportedGrantType,
			ErrorDescription: "grant_type must be 'client_credentials'",
		})
	}

	scope := c.FormValue("scope")

	// 客户端认证（RFC 4.4.2: client MUST authenticate）
	clientID, clientSecret, _ := authenticateClient(c)
	client, ok := clients[clientID]
	if !ok || client.Secret != clientSecret {
		return c.JSON(http.StatusUnauthorized, types.ErrorResponse{
			Error:            types.ErrorInvalidClient,
			ErrorDescription: "client authentication failed",
		})
	}

	// 生成 access token（RFC 4.4.3: refresh token SHOULD NOT be included）
	tokenValue, err := generateRandomString(32)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: types.ErrorServerError})
	}

	accessTokensMu.Lock()
	accessTokens[tokenValue] = AccessToken{
		Token:     tokenValue,
		ClientID:  clientID,
		Scope:     scope,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	accessTokensMu.Unlock()

	return c.JSON(http.StatusOK, types.AccessTokenResponse{
		AccessToken: tokenValue,
		TokenType:   "Bearer",
		ExpiresIn:   3600,
		Scope:       scope,
	})
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
		Exp:      t.ExpiresAt.Unix(),
	})
}
