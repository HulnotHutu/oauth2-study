package types

// TokenResponse 令牌端点响应（客户端使用）
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// IntrospectResponse introspection 端点响应（资源服务器使用）
type IntrospectResponse struct {
	Active   bool   `json:"active"`
	ClientID string `json:"client_id,omitempty"`
	Username string `json:"username,omitempty"`
	Exp      int64  `json:"exp,omitempty"`
}
