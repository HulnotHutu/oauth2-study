package types

// ──────────────────────────────────────────────
// Authorization Request (RFC 4.1.1)
// ──────────────────────────────────────────────

// AuthorizationRequest 授权请求参数
type AuthorizationRequest struct {
	ResponseType string `query:"response_type" form:"response_type"` // REQUIRED. MUST be "code"
	ClientID     string `query:"client_id" form:"client_id"`         // REQUIRED
	RedirectURI  string `query:"redirect_uri" form:"redirect_uri"`   // OPTIONAL
	Scope        string `query:"scope" form:"scope"`                 // OPTIONAL
	State        string `query:"state" form:"state"`                 // RECOMMENDED (CSRF)
}

// ──────────────────────────────────────────────
// Authorization Response (RFC 4.1.2)
// ──────────────────────────────────────────────

// AuthorizationResponse 授权成功响应（通过 redirect query 返回）
type AuthorizationResponse struct {
	Code  string `query:"code"`  // REQUIRED
	State string `query:"state"` // REQUIRED if "state" was in request
}

// ──────────────────────────────────────────────
// Error Response (RFC 4.1.2.1 / 5.2)
// ──────────────────────────────────────────────

// ErrorCode OAuth2 标准错误码
type ErrorCode string

const (
	// ── 授权端点错误 (4.1.2.1) ──
	ErrorInvalidRequest          ErrorCode = "invalid_request"
	ErrorUnauthorizedClient      ErrorCode = "unauthorized_client"
	ErrorAccessDenied            ErrorCode = "access_denied"
	ErrorUnsupportedResponseType ErrorCode = "unsupported_response_type"
	ErrorInvalidScope            ErrorCode = "invalid_scope"
	ErrorServerError             ErrorCode = "server_error"
	ErrorTemporarilyUnavailable  ErrorCode = "temporarily_unavailable"

	// ── 令牌端点错误 (5.2) ──
	ErrorInvalidClient        ErrorCode = "invalid_client"
	ErrorInvalidGrant         ErrorCode = "invalid_grant"
	ErrorUnsupportedGrantType ErrorCode = "unsupported_grant_type"
)

// ErrorResponse OAuth2 错误响应（JSON body / redirect query）
type ErrorResponse struct {
	Error            ErrorCode `json:"error"`                       // REQUIRED
	ErrorDescription string    `json:"error_description,omitempty"` // OPTIONAL
	ErrorURI         string    `json:"error_uri,omitempty"`         // OPTIONAL
	State            string    `json:"state,omitempty"`             // REQUIRED if "state" was in request
}

// ──────────────────────────────────────────────
// Access Token Request (RFC 4.1.3)
// ──────────────────────────────────────────────

// AccessTokenRequest 访问令牌请求参数
type AccessTokenRequest struct {
	GrantType   string `form:"grant_type"`   // REQUIRED. MUST be "authorization_code"
	Code        string `form:"code"`          // REQUIRED
	RedirectURI string `form:"redirect_uri"`  // REQUIRED if "redirect_uri" was in auth request
	ClientID    string `form:"client_id"`     // OPTIONAL (if using Basic auth header)
	ClientSecret string `form:"client_secret"` // OPTIONAL (if using Basic auth header)
}

// ──────────────────────────────────────────────
// Access Token Response (RFC 4.1.4 / 5.1)
// ──────────────────────────────────────────────

// AccessTokenResponse 访问令牌成功响应
type AccessTokenResponse struct {
	AccessToken  string `json:"access_token"`            // REQUIRED
	TokenType    string `json:"token_type"`              // REQUIRED
	ExpiresIn    int    `json:"expires_in"`              // RECOMMENDED
	RefreshToken string `json:"refresh_token,omitempty"` // OPTIONAL
	Scope        string `json:"scope,omitempty"`         // OPTIONAL
}

// ──────────────────────────────────────────────
// Introspect Response (Token Validation)
// ──────────────────────────────────────────────

// IntrospectResponse introspection 端点响应（资源服务器使用）
type IntrospectResponse struct {
	Active   bool   `json:"active"`
	ClientID string `json:"client_id,omitempty"`
	Username string `json:"username,omitempty"`
	Exp      int64  `json:"exp,omitempty"`
}
