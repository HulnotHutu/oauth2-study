package types

// ──────────────────────────────────────────────
// Authorization Request (RFC 4.1.1 + PKCE)
// ──────────────────────────────────────────────

type AuthorizationRequest struct {
	ResponseType        string `query:"response_type" form:"response_type"`
	ClientID            string `query:"client_id" form:"client_id"`
	RedirectURI         string `query:"redirect_uri" form:"redirect_uri"`
	Scope               string `query:"scope" form:"scope"`
	State               string `query:"state" form:"state"`
	CodeChallenge       string `query:"code_challenge" form:"code_challenge"`               // PKCE
	CodeChallengeMethod string `query:"code_challenge_method" form:"code_challenge_method"` // PKCE: "S256" or "plain"
}

type AuthorizationResponse struct {
	Code  string `query:"code"`
	State string `query:"state"`
}

// ──────────────────────────────────────────────
// Error Response (RFC 4.1.2.1 / 5.2)
// ──────────────────────────────────────────────

type ErrorCode string

const (
	ErrorInvalidRequest          ErrorCode = "invalid_request"
	ErrorUnauthorizedClient      ErrorCode = "unauthorized_client"
	ErrorAccessDenied            ErrorCode = "access_denied"
	ErrorUnsupportedResponseType ErrorCode = "unsupported_response_type"
	ErrorInvalidScope            ErrorCode = "invalid_scope"
	ErrorServerError             ErrorCode = "server_error"
	ErrorTemporarilyUnavailable  ErrorCode = "temporarily_unavailable"

	ErrorInvalidClient        ErrorCode = "invalid_client"
	ErrorInvalidGrant         ErrorCode = "invalid_grant"
	ErrorUnsupportedGrantType ErrorCode = "unsupported_grant_type"
)

type ErrorResponse struct {
	Error            ErrorCode `json:"error"`
	ErrorDescription string    `json:"error_description,omitempty"`
	ErrorURI         string    `json:"error_uri,omitempty"`
	State            string    `json:"state,omitempty"`
}

// ──────────────────────────────────────────────
// Access Token Request (RFC 4.1.3 + PKCE)
// ──────────────────────────────────────────────

type AccessTokenRequest struct {
	GrantType    string `form:"grant_type"`    // REQUIRED. MUST be "authorization_code"
	Code         string `form:"code"`           // REQUIRED
	RedirectURI  string `form:"redirect_uri"`   // REQUIRED if in auth request
	ClientID     string `form:"client_id"`      // OPTIONAL (if using Basic auth)
	ClientSecret string `form:"client_secret"`  // OPTIONAL
	CodeVerifier string `form:"code_verifier"`  // PKCE: REQUIRED if code_challenge was sent
}

// ──────────────────────────────────────────────
// Access Token Response (RFC 4.1.4 / 5.1)
// ──────────────────────────────────────────────

type AccessTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ──────────────────────────────────────────────
// Refresh Token Request (RFC 6)
// ──────────────────────────────────────────────

type RefreshTokenRequest struct {
	GrantType    string `form:"grant_type"`
	RefreshToken string `form:"refresh_token"`
	Scope        string `form:"scope"`
}

// ──────────────────────────────────────────────
// Introspect Response
// ──────────────────────────────────────────────

type IntrospectResponse struct {
	Active   bool   `json:"active"`
	ClientID string `json:"client_id,omitempty"`
	Username string `json:"username,omitempty"`
	Exp      int64  `json:"exp,omitempty"`
}
