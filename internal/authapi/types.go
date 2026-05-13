package authapi

// tokenRequest captures the union of fields used by the four supported grants.
// /oauth/token accepts both application/x-www-form-urlencoded and JSON bodies.
type tokenRequest struct {
	GrantType    string `json:"grant_type" form:"grant_type"`
	ClientID     string `json:"client_id" form:"client_id"`
	ClientSecret string `json:"client_secret" form:"client_secret"`
	Audience     string `json:"audience" form:"audience"`
	Scope        string `json:"scope" form:"scope"`
	Username     string `json:"username" form:"username"`
	Password     string `json:"password" form:"password"`
	RefreshToken string `json:"refresh_token" form:"refresh_token"`
	Code         string `json:"code" form:"code"`
	RedirectURI  string `json:"redirect_uri" form:"redirect_uri"`
	CodeVerifier string `json:"code_verifier" form:"code_verifier"`
	Realm        string `json:"realm" form:"realm"`
}

// tokenResponse is the JSON body returned for a successful /oauth/token call.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
}
