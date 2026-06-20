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

	"OAuth2/cmd/Resource-Owner-Password-Credentials/types"
)

// Client 注册的客户端信息
type Client struct {
	ID     string
	Secret string
}

// User 内置的用户账号
type User struct {
	Username string
	Password string
}

// AccessToken 访问令牌
type AccessToken struct {
	Token     string
	ClientID  string
	Username  string
	Scope     string
	ExpiresAt time.Time
}

// RefreshToken 刷新令牌
type RefreshToken struct {
	Token     string
	ClientID  string
	Username  string
	Scope     string
	ExpiresAt time.Time
}

// rotatedTokenRecord 记录已轮换的 refresh token（重放检测）
type rotatedTokenRecord struct {
	replacedBy string
}

var (
	clients = map[string]Client{
		"ropc-client-1": {
			ID:     "ropc-client-1",
			Secret: "ropc-client-secret-1",
		},
	}

	// 内置用户（生产环境应使用数据库）
	users = map[string]User{
		"alice": {Username: "alice", Password: "password123"},
		"bob":   {Username: "bob", Password: "secret456"},
	}

	accessTokens   = map[string]AccessToken{}
	accessTokensMu sync.RWMutex

	refreshTokens   = map[string]RefreshToken{}
	refreshTokensMu sync.RWMutex

	usedRefreshTokens   = map[string]rotatedTokenRecord{}
	usedRefreshTokensMu sync.RWMutex
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

// handleToken 处理令牌请求（RFC 4.3.2 / 4.3.3）
func handleToken(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Pragma", "no-cache")

	grantType := c.FormValue("grant_type")

	switch grantType {
	case "password":
		return handlePasswordGrant(c)
	case "refresh_token":
		return handleRefreshToken(c)
	default:
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorUnsupportedGrantType,
			ErrorDescription: "grant_type must be 'password' or 'refresh_token'",
		})
	}
}

// handlePasswordGrant 处理 password grant_type（RFC 4.3.2）
func handlePasswordGrant(c *echo.Context) error {
	username := c.FormValue("username")
	password := c.FormValue("password")
	scope := c.FormValue("scope")

	// 验证用户名密码
	user, ok := users[username]
	if !ok || user.Password != password {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "invalid username or password",
		})
	}

	// 客户端认证
	clientID, clientSecret, _ := authenticateClient(c)
	client, ok := clients[clientID]
	if !ok || client.Secret != clientSecret {
		return c.JSON(http.StatusUnauthorized, types.ErrorResponse{
			Error:            types.ErrorInvalidClient,
			ErrorDescription: "client authentication failed",
		})
	}

	// 生成 access token
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

	// 生成 refresh token
	refreshTokenValue, err := generateRandomString(32)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: types.ErrorServerError})
	}

	refreshTokensMu.Lock()
	refreshTokens[refreshTokenValue] = RefreshToken{
		Token:     refreshTokenValue,
		ClientID:  clientID,
		Username:  username,
		Scope:     scope,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}
	refreshTokensMu.Unlock()

	return c.JSON(http.StatusOK, types.AccessTokenResponse{
		AccessToken:  tokenValue,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: refreshTokenValue,
	})
}

// handleRefreshToken 处理 refresh_token grant_type
func handleRefreshToken(c *echo.Context) error {
	refreshTokenValue := c.FormValue("refresh_token")
	scope := c.FormValue("scope")

	clientID, clientSecret, _ := authenticateClient(c)

	client, ok := clients[clientID]
	if !ok || client.Secret != clientSecret {
		return c.JSON(http.StatusUnauthorized, types.ErrorResponse{
			Error:            types.ErrorInvalidClient,
			ErrorDescription: "client authentication failed",
		})
	}

	refreshTokensMu.RLock()
	rt, ok := refreshTokens[refreshTokenValue]
	refreshTokensMu.RUnlock()

	if !ok {
		// 检查是否为已轮换的 token（重放检测）
		usedRefreshTokensMu.RLock()
		used, wasRotated := usedRefreshTokens[refreshTokenValue]
		usedRefreshTokensMu.RUnlock()

		if wasRotated {
			refreshTokensMu.Lock()
			activeRT, stillActive := refreshTokens[used.replacedBy]
			if stillActive {
				accessTokensMu.Lock()
				var tokensToRevoke []string
				for k, v := range accessTokens {
					if v.ClientID == activeRT.ClientID && v.Username == activeRT.Username {
						tokensToRevoke = append(tokensToRevoke, k)
					}
				}
				for _, tid := range tokensToRevoke {
					delete(accessTokens, tid)
				}
				accessTokensMu.Unlock()
				delete(refreshTokens, used.replacedBy)
			}
			refreshTokensMu.Unlock()
			return c.JSON(http.StatusBadRequest, types.ErrorResponse{
				Error:            types.ErrorInvalidGrant,
				ErrorDescription: "refresh token replay detected; all tokens have been revoked",
			})
		}

		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "refresh token not found",
		})
	}

	if rt.ClientID != clientID {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "refresh token was issued for a different client",
		})
	}
	if time.Now().After(rt.ExpiresAt) {
		refreshTokensMu.Lock()
		delete(refreshTokens, refreshTokenValue)
		refreshTokensMu.Unlock()
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{
			Error:            types.ErrorInvalidGrant,
			ErrorDescription: "refresh token has expired",
		})
	}

	requestedScope := scope
	if requestedScope == "" {
		requestedScope = rt.Scope
	}

	newTokenValue, err := generateRandomString(32)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: types.ErrorServerError})
	}

	accessTokensMu.Lock()
	accessTokens[newTokenValue] = AccessToken{
		Token:     newTokenValue,
		ClientID:  rt.ClientID,
		Username:  rt.Username,
		Scope:     requestedScope,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	accessTokensMu.Unlock()

	newRefreshTokenValue, err := generateRandomString(32)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: types.ErrorServerError})
	}

	refreshTokensMu.Lock()
	usedRefreshTokensMu.Lock()
	usedRefreshTokens[refreshTokenValue] = rotatedTokenRecord{replacedBy: newRefreshTokenValue}
	usedRefreshTokensMu.Unlock()
	delete(refreshTokens, refreshTokenValue)
	refreshTokens[newRefreshTokenValue] = RefreshToken{
		Token:     newRefreshTokenValue,
		ClientID:  rt.ClientID,
		Username:  rt.Username,
		Scope:     requestedScope,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}
	refreshTokensMu.Unlock()

	return c.JSON(http.StatusOK, types.AccessTokenResponse{
		AccessToken:  newTokenValue,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: newRefreshTokenValue,
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
		Username: t.Username,
		Exp:      t.ExpiresAt.Unix(),
	})
}
