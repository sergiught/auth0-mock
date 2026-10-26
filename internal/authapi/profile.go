package authapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DefaultSubject is the user the authorization-code flow logs in when
// /authorize carries no login_hint.
const DefaultSubject = "auth0|mock-user"

// profileOf picks the profile keys present in an id_token Extra.
func profileOf(extra map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"email", "email_verified", "name", "nickname", "picture", "phone_number", "phone_number_verified"} {
		if v, ok := extra[k]; ok {
			out[k] = v
		}
	}
	return out
}

// deriveProfile builds a deterministic id_token profile from a subject, so the
// same user always looks the same across flows.
func deriveProfile(subject string) map[string]any {
	local := subject
	if i := strings.LastIndex(local, "|"); i >= 0 {
		local = local[i+1:]
	}
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	email := subject
	if !strings.Contains(subject, "@") {
		email = local + "@example.com"
	}
	initials := []rune(strings.ToLower(local))
	if len(initials) > 2 {
		initials = initials[:2]
	}
	return map[string]any{
		"email":          email,
		"email_verified": true,
		"name":           email,
		"nickname":       local,
		"picture":        "https://cdn.auth0.com/avatars/" + string(initials) + ".png",
	}
}

// fillProfile adds the derived profile keys that extra does not already set
// and returns extra.
func fillProfile(extra map[string]any, subject string) map[string]any {
	for k, v := range deriveProfile(subject) {
		if _, ok := extra[k]; !ok {
			extra[k] = v
		}
	}
	return extra
}

// passwordlessUser returns the stable subject and id_token profile for a
// passwordless login. The subject is derived from the normalised identifier, so
// the same address or number is always the same user.
func passwordlessUser(realm, username string) (subject string, profile map[string]any) {
	trimmed := strings.TrimSpace(username)
	sum := sha256.Sum256([]byte(strings.ToLower(trimmed)))
	subject = realm + "|" + hex.EncodeToString(sum[:])[:24]
	if realm == "sms" {
		return subject, map[string]any{
			"phone_number":          trimmed,
			"phone_number_verified": true,
			"name":                  trimmed,
			"nickname":              trimmed,
			"picture":               "https://cdn.auth0.com/avatars/default.png",
		}
	}
	return subject, deriveProfile(trimmed)
}
