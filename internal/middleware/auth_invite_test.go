package middleware

import "testing"

func TestInviteLoginAllowsOnlyAnonymousPost(t *testing.T) {
	if !isNoAuthAPI("/api/v1/auth/login-by-invite", "POST") {
		t.Fatal("invite login must be available before authentication")
	}
	if isNoAuthAPI("/api/v1/auth/login-by-invite", "GET") || isNoAuthAPI("/api/v1/auth/login-by-invite/other", "POST") {
		t.Fatal("invite login must not make unrelated routes public")
	}
}
