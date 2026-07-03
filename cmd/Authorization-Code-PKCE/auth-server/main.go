package main

import (
	"crypto/rand"
	"crypto/sha256"
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

	"OAuth2/cmd/Authorization-Code-PKCE/types"
)

type Client struct {
	ID           string
	Secret       string
	RedirectURIs []string
}

// AuthorizationCode 包含 PKCE 的 challenge 信息
type AuthorizationCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	Username            string
	Scope               string
	ExpiresAt           time.Time
	CodeChallenge       string // PKCE: code_challenge from auth request
	CodeChallengeMethod string // PKCE: "S256" or "plain"
}

type AccessToken struct {
	Token     string
	ClientID  string
	Username  string
	Scope     string
	ExpiresAt time.Time
}

type RefreshToken struct {
	Token     string
	ClientID  string
	Username  string
	Scope     string
	ExpiresAt time.Time
}

type codeRecord struct {
	tokenIDs []string
}

type rotatedTokenRecord struct {
	replacedBy string
}

// verifyPKCE 验证 code_verifier 与存储的 code_challenge 是否匹配（RFC 7636）
func verifyPKCE(verifier, challenge, method string) bool {
	switch method {
	case "S256":
		hash := sha256.Sum256([]byte(verifier))
		expected := base64.RawURLEncoding.EncodeToString(hash[:])
		return expected == challenge
	case "plain":
		return verifier == challenge
	default:
		return false
	}
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

func errorRedirect(c *echo.Context, redirectURI, state string, code types.ErrorCode, desc string) error {
	loc := fmt.Sprintf("%s?error=%s&error_description=%s", redirectURI,
		url.QueryEscape(string(code)), url.QueryEscape(desc))
	if state != "" {
		loc += "&state=" + url.QueryEscape(state)
	}
	return c.Redirect(http.StatusFound, loc)
}

func revokeTokens(tokenIDs []string) {
	accessTokensMu.Lock()
	defer accessTokensMu.Unlock()
	for _, tid := range tokenIDs {
		delete(accessTokens, tid)
	}
}

func main() {
	e := echo.New()

	viewsDir, _ := filepath.Abs(filepath.Join("cmd", "Authorization-Code-PKCE", "views"))
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

// handleAuthorize 解析 PKCE 参数并展示登录页面
func handleAuthorize(c *echo.Context) error {
	clientID := c.QueryParam("client_id")
	redirectURI := c.QueryParam("redirect_uri")
	responseType := c.QueryParam("response_type")
	state := c.QueryParam("state")
	codeChallenge := c.QueryParam("code_challenge")
	codeChallengeMethod := c.QueryParam("code_challenge_method")

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

	// 验证 PKCE code_challenge_method（RFC 7636 Section 4.4.2）
	if codeChallenge != "" && codeChallengeMethod == "" {
		codeChallengeMethod = "plain" // 默认 plain（RFC 7636）
	}
	if codeChallengeMethod != "" && codeChallengeMethod != "S256" && codeChallengeMethod != "plain" {
		return errorRedirect(c, redirectURI, state, types.ErrorInvalidRequest,
			"unsupported code_challenge_method, must be 'S256' or 'plain'")
	}

	return c.Render(http.StatusOK, "authorize.html", map[string]string{
		"ClientID":            clientID,
		"RedirectURI":         redirectURI,
		"ResponseType":        responseType,
		"State":               state,
		"CodeChallenge":       codeChallenge,
		"CodeChallengeMethod": codeChallengeMethod,
	})
}

// handleLogin 存储 PKCE challenge 并签发授权码
func handleLogin(c *echo.Context) error {
	clientID := c.FormValue("client_id")
	redirectURI := c.FormValue("redirect_uri")
	state := c.FormValue("state")
	approve := c.FormValue("approve")
	username := c.FormValue("username")
	password := c.FormValue("password")
	codeChallenge := c.FormValue("code_challenge")
	codeChallengeMethod := c.FormValue("code_challenge_method")

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
		Code:                code,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Username:            username,
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
	}
	authCodesMu.Unlock()

	location := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, code, state)
	return c.Redirect(http.StatusFound, location)
}

// handleToken 验证 PKCE code_verifier 并签发令牌
func handleToken(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Pragma", "no-cache")

	grantType := c.FormValue("grant_type")

	if grantType == "refresh_token" {
		return handleRefreshToken(c)
	}

	code := c.FormValue("code")
	redirectURI := c.FormValue("redirect_uri")
	codeVerifier := c.FormValue("code_verifier")

	clientID, clientSecret, _ := authenticateClient(c)

	if grantType != "authorization_code" {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{Error: types.ErrorUnsupportedGrantType})
	}

	client, ok := clients[clientID]
	if !ok || client.Secret != clientSecret {
		return c.JSON(http.StatusUnauthorized, types.ErrorResponse{Error: types.ErrorInvalidClient})
	}

	// 检查授权码重用
	usedCodesMu.RLock()
	used, wasUsed := usedCodes[code]
	usedCodesMu.RUnlock()

	if wasUsed {
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

	// PKCE code_verifier 验证（RFC 7636 Section 4.6）
	if authCode.CodeChallenge != "" {
		// 授权请求携带了 code_challenge → 必须验证 code_verifier（防降级攻击）
		if codeVerifier == "" {
			return c.JSON(http.StatusBadRequest, types.ErrorResponse{
				Error:            types.ErrorInvalidGrant,
				ErrorDescription: "code_verifier is required (PKCE)",
			})
		}
		if !verifyPKCE(codeVerifier, authCode.CodeChallenge, authCode.CodeChallengeMethod) {
			return c.JSON(http.StatusBadRequest, types.ErrorResponse{
				Error:            types.ErrorInvalidGrant,
				ErrorDescription: "code_verifier verification failed (PKCE)",
			})
		}
	}
	// 如果授权请求没有 code_challenge，则跳过 PKCE 验证（兼容非 PKCE 客户端）

	username := authCode.Username
	scope := authCode.Scope

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

	usedCodesMu.Lock()
	usedCodes[code] = codeRecord{tokenIDs: []string{tokenValue}}
	usedCodesMu.Unlock()

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

// handleRefreshToken 处理 refresh_token grant_type 请求
func handleRefreshToken(c *echo.Context) error {
	refreshTokenValue := c.FormValue("refresh_token")
	scope := c.FormValue("scope")

	clientID, clientSecret, _ := authenticateClient(c)

	client, ok := clients[clientID]
	if !ok || client.Secret != clientSecret {
		return c.JSON(http.StatusUnauthorized, types.ErrorResponse{Error: types.ErrorInvalidClient})
	}

	refreshTokensMu.RLock()
	rt, ok := refreshTokens[refreshTokenValue]
	refreshTokensMu.RUnlock()

	if !ok {
		usedRefreshTokensMu.RLock()
		used, wasRotated := usedRefreshTokens[refreshTokenValue]
		usedRefreshTokensMu.RUnlock()

		if wasRotated {
			refreshTokensMu.Lock()
			activeRT, stillActive := refreshTokens[used.replacedBy]
			if stillActive {
				var tokensToRevoke []string
				accessTokensMu.Lock()
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

func handleClientInfo(c *echo.Context) error {
	return c.Render(http.StatusOK, "client_info.html", nil)
}
