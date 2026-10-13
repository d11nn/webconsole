package WebUI

import (
	"bytes"
	"context"
	"github.com/free5gc/webconsole/backend/deployment"
	"github.com/free5gc/webconsole/backend/logger"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAgentHTTPBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	previous := DeploymentService
	defer func() { DeploymentService = previous }()
	DeploymentService = &deployment.Service{Store: deployment.NewMemoryStore(), Plan: deployment.Plan{Host: "test", Commit: strings.Repeat("a", 40), MockOnly: true}}
	admin := deployment.Principal{UserID: "owner", TenantID: "tenant"}
	_ = DeploymentService.Bootstrap(ctx, "owner", "tenant")
	a, token, _ := DeploymentService.CreateAgent(ctx, admin, "agent", "test", time.Now().Add(time.Hour))
	p := deployment.Principal{UserID: "owner", TenantID: "tenant", AgentID: a.ID}
	r, _ := DeploymentService.CreateRequest(ctx, p, "eth0")
	router := gin.New()
	group := router.Group("/api")
	group.Use(AuthMiddleware())
	AddDeploymentRoutes(group)
	group.GET("/subscriber", func(c *gin.Context) { c.JSON(200, "should not be reached") })
	cases := []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/deployment-requests", `{"interface":"eth0","role":"Admin","approved":true}`, 400},
		{"POST", "/api/deployment-requests/" + r.ID + "/approve-and-start", `{"planHash":"` + r.PlanHash + `"}`, 403},
		{"POST", "/api/deployment-requests/" + r.ID + "/reject", `{}`, 403},
		{"GET", "/api/roles", "", 403},
		{"GET", "/api/agent-access", "", 403},
		{"GET", "/api/subscriber", "", 403},
		{"POST", "/api/deployment-requests/" + r.ID + "/start", `{}`, 404},
		{"GET", "/api/deployment-requests/" + r.ID, "", 200},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), token) {
			t.Fatal("credential leak")
		}
	}
	_ = DeploymentService.Revoke(ctx, admin, a.ID)
	req := httptest.NewRequest(http.MethodGet, "/api/deployment-requests", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("revoked credential accepted")
	}
}

func TestPanicLoggingRedactsAllCredentials(t *testing.T) {
	previous := logger.GinLog.Logger.Out
	var captured bytes.Buffer
	logger.GinLog.Logger.SetOutput(&captured)
	defer logger.GinLog.Logger.SetOutput(previous)
	router := NewRouter()
	router.GET("/panic", func(c *gin.Context) { panic("password-secret-value") })
	req := httptest.NewRequest("GET", "/panic?token=jwt-secret-value", nil)
	req.Header.Set("Token", "jwt-secret-value")
	req.Header.Set("Authorization", "Bearer agent-secret-value")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 500 {
		t.Fatal("panic recovery failed")
	}
	for _, secret := range []string{"password-secret-value", "jwt-secret-value", "agent-secret-value"} {
		if strings.Contains(captured.String(), secret) || strings.Contains(w.Body.String(), secret) {
			t.Fatal("secret leaked on panic")
		}
	}
}
