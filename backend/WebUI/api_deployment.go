package WebUI

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/free5gc/util/mongoapi"
	"github.com/free5gc/webconsole/backend/deployment"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

var DeploymentService *deployment.Service

// InitDeployment is opt-in and never installs software or enables privileged services.
func InitDeployment(ctx context.Context, dbName string) error {
	if os.Getenv("FREE5GC_DEPLOYMENT_MODE") == "" {
		return nil
	}
	if os.Getenv("FREE5GC_DEPLOYMENT_MODE") != "mock" {
		return errors.New("only mock deployment is supported; real worker is disabled")
	}
	host := os.Getenv("FREE5GC_DEPLOYMENT_HOST")
	commit := os.Getenv("FREE5GC_DEPLOYMENT_COMMIT")
	hash := os.Getenv("FREE5GC_DEPLOYMENT_SCRIPT_HASH")
	if host == "" || len(commit) != 40 || len(hash) != 64 {
		return errors.New("set deployment host, pinned commit and script SHA256")
	}
	store, err := deployment.NewMongoStore(ctx, mongoapi.Client.Database(dbName))
	if err != nil {
		return err
	}
	DeploymentService = &deployment.Service{Store: store, Plan: deployment.Plan{Host: host, Commit: commit, ScriptHash: hash, Profile: "mock-v1", Workdir: "no-installation-mock", MockOnly: true, Changes: []string{"安裝套件與 Go 升級（模擬）", "gtp5g、forwarding、UFW（模擬）", "設定檔與編譯（模擬）", "不執行安裝、不修改主機、不驗證 UE 上網"}}}
	return nil
}
func deploymentPath(path string) bool {
	for _, prefix := range []string{"/api/deployment-requests", "/api/deployment-jobs", "/api/agent-access", "/api/roles", "/api/rbac/me", "/api/deployment-audit"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
func deploymentPrincipal(c *gin.Context) (deployment.Principal, error) {
	if p, ok := c.Get("deploymentPrincipal"); ok {
		return p.(deployment.Principal), nil
	}
	claims, err := getAuthenticatedClaims(c)
	if err != nil {
		return deployment.Principal{}, deployment.ErrDenied
	}
	uid, ok := getClaimString(claims, "sub")
	if !ok {
		return deployment.Principal{}, deployment.ErrDenied
	}
	tenant, ok := getClaimString(claims, "tenantId")
	if !ok {
		return deployment.Principal{}, deployment.ErrDenied
	}
	user, err := mongoapi.RestfulAPIGetOne(userDataColl, bson.M{"userId": uid, "tenantId": tenant})
	if err != nil || len(user) == 0 {
		return deployment.Principal{}, deployment.ErrDenied
	}
	return deployment.Principal{UserID: uid, TenantID: tenant}, nil
}
func depError(c *gin.Context, err error) {
	code := http.StatusBadRequest
	message := err.Error()
	switch {
	case errors.Is(err, deployment.ErrDenied):
		code = http.StatusForbidden
	case errors.Is(err, deployment.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, deployment.ErrConflict):
		code = http.StatusConflict
	}
	// DB/driver failures must not expose connection details or credential material.
	if code == http.StatusBadRequest && !strings.HasPrefix(message, "invalid") && !strings.HasPrefix(message, "request") && !strings.HasPrefix(message, "plan") && !strings.HasPrefix(message, "host") && !strings.HasPrefix(message, "cannot") {
		code = 503
		message = "authorization storage unavailable"
	}
	c.JSON(code, gin.H{"error": message})
}
func depBind(c *gin.Context, v interface{}) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		c.JSON(400, gin.H{"error": "invalid request fields"})
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return false
	}
	return true
}
func AddDeploymentRoutes(group *gin.RouterGroup) {
	g := group.Group("")
	g.Use(func(c *gin.Context) {
		if DeploymentService == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "deployment integration disabled"})
			return
		}
		p, e := deploymentPrincipal(c)
		if e != nil {
			c.AbortWithStatusJSON(403, gin.H{"error": "permission denied"})
			return
		}
		c.Set("deploymentPrincipal", p)
		required := ""
		switch {
		case strings.HasPrefix(c.Request.URL.Path, "/api/roles") || c.Request.URL.Path == "/api/deployment-audit":
			required = "role:manage"
		case strings.HasPrefix(c.Request.URL.Path, "/api/agent-access"):
			required = "agent:manage"
		case strings.HasSuffix(c.Request.URL.Path, "/approve-and-start") || strings.HasSuffix(c.Request.URL.Path, "/reject"):
			required = "deployment:approve"
		}
		if required != "" && (p.AgentID != "" || !DeploymentService.Can(c, p, required)) {
			c.AbortWithStatusJSON(403, gin.H{"error": "permission denied"})
			return
		}
		c.Next()
	})
	principal := func(c *gin.Context) deployment.Principal { p, _ := deploymentPrincipal(c); return p }
	g.GET("/rbac/me", func(c *gin.Context) {
		p := principal(c)
		permissions, e := DeploymentService.PermissionsFor(c, p)
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, gin.H{"userId": p.UserID, "tenantId": p.TenantID, "agentId": p.AgentID, "permissions": permissions})
	})
	g.GET("/roles", func(c *gin.Context) {
		a, e := DeploymentService.Roles(c, principal(c))
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, gin.H{"roles": deployment.Permissions, "assignments": a})
	})
	g.POST("/roles", func(c *gin.Context) {
		var a deployment.Assignment
		if !depBind(c, &a) {
			return
		}
		u, e := mongoapi.RestfulAPIGetOne(userDataColl, bson.M{"userId": a.UserID, "tenantId": a.TenantID})
		if e != nil || len(u) == 0 {
			c.JSON(400, gin.H{"error": "invalid user scope"})
			return
		}
		if a.Role == "Admin" {
			hash, _ := u["encryptedPassword"].(string)
			if hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("free5gc")) == nil {
				c.JSON(400, gin.H{"error": "invalid owner credentials; change the default password first"})
				return
			}
		}
		if e = DeploymentService.Assign(c, principal(c), a); e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	g.GET("/deployment-audit", func(c *gin.Context) {
		v, e := DeploymentService.AuditEvents(c, principal(c))
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, v)
	})
	g.GET("/agent-access", func(c *gin.Context) {
		v, e := DeploymentService.Agents(c, principal(c))
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, v)
	})
	g.POST("/agent-access", func(c *gin.Context) {
		var in struct {
			Name    string    `json:"name"`
			Host    string    `json:"host"`
			Expires time.Time `json:"expiresAt"`
		}
		if !depBind(c, &in) {
			return
		}
		a, t, e := DeploymentService.CreateAgent(c, principal(c), in.Name, in.Host, in.Expires)
		if e != nil {
			depError(c, e)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(201, gin.H{"agent": a, "token": t})
	})
	g.POST("/agent-access/:id/revoke", func(c *gin.Context) {
		if e := DeploymentService.Revoke(c, principal(c), c.Param("id")); e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	g.GET("/deployment-requests", func(c *gin.Context) {
		v, e := DeploymentService.Requests(c, principal(c))
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, v)
	})
	g.POST("/deployment-requests", func(c *gin.Context) {
		var in struct {
			Interface string `json:"interface"`
		}
		if !depBind(c, &in) {
			return
		}
		v, e := DeploymentService.CreateRequest(c, principal(c), in.Interface)
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(201, gin.H{"request": v, "approvalUrl": "/deployments/" + v.ID})
	})
	g.GET("/deployment-requests/:id", func(c *gin.Context) {
		r, j, e := DeploymentService.Get(c, principal(c), c.Param("id"))
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, gin.H{"request": r, "job": j})
	})
	g.POST("/deployment-requests/:id/approve-and-start", func(c *gin.Context) {
		var in struct {
			PlanHash string `json:"planHash"`
		}
		if !depBind(c, &in) {
			return
		}
		j, e := DeploymentService.Approve(c, principal(c), c.Param("id"), in.PlanHash)
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, j)
	})
	g.POST("/deployment-requests/:id/reject", func(c *gin.Context) {
		if e := DeploymentService.Reject(c, principal(c), c.Param("id")); e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	g.GET("/deployment-jobs/:id", func(c *gin.Context) {
		j, e := DeploymentService.GetJob(c, principal(c), c.Param("id"))
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, j)
	})
	g.GET("/deployment-jobs/:id/logs", func(c *gin.Context) {
		j, e := DeploymentService.GetJob(c, principal(c), c.Param("id"))
		if e != nil {
			depError(c, e)
			return
		}
		c.JSON(200, gin.H{"logs": j.Logs, "updatedAt": j.Updated})
	})
}

// Legacy CRUD must not grant access to host owner credentials indirectly.
func guardLegacyRBAC(c *gin.Context, claims map[string]interface{}, tenant string) bool {
	if DeploymentService == nil {
		return true
	}
	uid, _ := claims["sub"].(string)
	p := deployment.Principal{UserID: uid, TenantID: tenant}
	if c.Request.URL.Path != "/api/tenant" && !strings.HasPrefix(c.Request.URL.Path, "/api/tenant/") {
		return true
	}
	hostAdmin := DeploymentService.Can(c, p, "role:manage")
	targetTenant := c.Param("tenantId")
	targetUser := c.Param("userId")
	if DeploymentService.Can(c, p, "tenant:manage:tenant") && !DeploymentService.Can(c, p, "subscriber:manage") {
		if c.Request.Method == http.MethodGet && c.Request.URL.Path == "/api/tenant" {
			c.Set("tenantScopedAdmin", true)
			c.Set(authAdminContextKey, true)
		} else if targetTenant == tenant {
			c.Set(authAdminContextKey, true)
		} else {
			c.AbortWithStatusJSON(403, gin.H{"error": "tenant scope denied"})
			return false
		}
	}
	if targetTenant != "" && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodOptions {
		protected, e := DeploymentService.HostProtected(c, targetTenant, targetUser)
		if e != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "authorization storage unavailable"})
			return false
		}
		if protected && (!hostAdmin || c.Request.Method == http.MethodDelete) {
			c.AbortWithStatusJSON(403, gin.H{"error": "host owner credentials protected; use role management first"})
			return false
		}
	}
	return true
}
