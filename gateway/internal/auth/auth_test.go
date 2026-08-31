package auth

import (
	"testing"
	"time"

	"github.com/yafeng/mcp-marketplace/gateway/internal/model"
)

func TestDefaultRoleAllows(t *testing.T) {
	cases := []struct {
		role, action string
		want         bool
	}{
		{model.RoleAdmin, model.ActionInvoke, true},
		{model.RoleAdmin, model.ActionManage, true},
		{model.RoleAdmin, model.ActionRead, true},
		{model.RoleDeveloper, model.ActionInvoke, true},
		{model.RoleDeveloper, model.ActionRead, true},
		{model.RoleDeveloper, model.ActionManage, true},
		{model.RoleViewer, model.ActionRead, true},
		{model.RoleViewer, model.ActionInvoke, false},
		{model.RoleViewer, model.ActionManage, false},
		{"unknown", model.ActionInvoke, false},
	}
	for _, c := range cases {
		if got := defaultRoleAllows(c.role, c.action); got != c.want {
			t.Errorf("defaultRoleAllows(%q,%q)=%v want %v", c.role, c.action, got, c.want)
		}
	}
}

func TestJWTRoundTrip(t *testing.T) {
	secret := "test-secret-at-least-16-chars!"
	token, exp, err := IssueToken(secret, time.Hour, 1, "admin", model.RoleAdmin)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := ParseToken(secret, token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Username != "admin" || claims.Role != model.RoleAdmin || claims.UserID != 1 {
		t.Fatalf("bad claims: %+v", claims)
	}
	if !exp.After(time.Now()) {
		t.Fatal("expiration should be in future")
	}
	if _, err := ParseToken("wrong-secret-1234567890!", token); err == nil {
		t.Fatal("wrong secret should fail")
	}
}

