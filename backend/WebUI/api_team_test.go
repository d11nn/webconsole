package WebUI

import (
	"context"
	"encoding/json"
	"github.com/free5gc/webconsole/backend/deployment"
	"github.com/free5gc/webconsole/backend/team"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTeamRBACRejectsAgentAndUnauthorizedHumanRuns(t *testing.T) {
	oldTeam, oldDep := TeamService, DeploymentService
	defer func() { TeamService = oldTeam; DeploymentService = oldDep }()
	DeploymentService = &deployment.Service{Store: deployment.NewMemoryStore()}
	_ = DeploymentService.Bootstrap(context.Background(), "owner", "tenant")
	TeamService = &team.Service{Catalog: json.RawMessage(`{"agents":[]}`)}
	for _, tc := range []struct {
		name, method, path string
		p                  deployment.Principal
		expected           int
	}{
		{"owner can inspect", "GET", "/api/agent-team/catalog", deployment.Principal{UserID: "owner", TenantID: "tenant"}, 200},
		{"ordinary user cannot spend owner model quota", "POST", "/api/agent-team/runs", deployment.Principal{UserID: "ordinary", TenantID: "tenant"}, 403},
		{"agent credential is not human session", "GET", "/api/agent-team/catalog", deployment.Principal{UserID: "owner", TenantID: "tenant", AgentID: "agent"}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			g := r.Group("/api")
			g.Use(func(c *gin.Context) { c.Set("deploymentPrincipal", tc.p) })
			AddTeamRoutes(g)
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"prompt":"test","role":"Admin"}`))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			if w.Code != tc.expected {
				t.Fatalf("expected %d, got %d: %s", tc.expected, w.Code, w.Body.String())
			}
		})
	}
}
