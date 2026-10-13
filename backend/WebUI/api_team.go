package WebUI

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/free5gc/util/mongoapi"
	"github.com/free5gc/webconsole/backend/team"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"net/http"
	"os"
	"path/filepath"
)

var TeamService *team.Service

func InitTeam(ctx context.Context, dbName string) error {
	if os.Getenv("FREE5GC_TEAM_MODE") == "" {
		return nil
	}
	if os.Getenv("FREE5GC_TEAM_MODE") != "codex" || DeploymentService == nil {
		return errors.New("team runtime requires codex mode and initialized RBAC")
	}
	runtime := os.Getenv("FREE5GC_TEAM_RUNTIME")
	if runtime == "" {
		runtime = "tools/agent_team.py"
	}
	runtime, e := filepath.Abs(runtime)
	if e != nil {
		return e
	}
	if _, e = os.Stat(runtime); e != nil {
		return e
	}
	catalog, e := os.ReadFile(filepath.Join(filepath.Dir(runtime), "team_catalog.json"))
	if e != nil {
		return e
	}
	if !json.Valid(catalog) {
		return errors.New("invalid team catalog")
	}
	TeamService, e = team.New(ctx, mongoapi.Client.Database(dbName), runtime, catalog)
	return e
}
func AddTeamRoutes(group *gin.RouterGroup) {
	g := group.Group("/agent-team")
	g.Use(func(c *gin.Context) {
		if TeamService == nil || DeploymentService == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "Codex team runtime disabled"})
			return
		}
		p, e := deploymentPrincipal(c)
		if e != nil || p.AgentID != "" {
			c.AbortWithStatusJSON(403, gin.H{"error": "human login required"})
			return
		}
		perm := "team:read"
		if c.Request.Method == http.MethodPost {
			perm = "team:run"
		}
		if !DeploymentService.Can(c, p, perm) {
			c.AbortWithStatusJSON(403, gin.H{"error": "team permission denied"})
			return
		}
		c.Set("deploymentPrincipal", p)
		c.Next()
	})
	g.GET("/catalog", func(c *gin.Context) { c.Data(200, "application/json", TeamService.Catalog) })
	g.GET("/runs", func(c *gin.Context) {
		p, _ := deploymentPrincipal(c)
		r, e := TeamService.List(c, p.UserID, p.TenantID, DeploymentService.Can(c, p, "team:run"))
		if e != nil {
			c.JSON(503, gin.H{"error": "team storage unavailable"})
			return
		}
		c.JSON(200, r)
	})
	g.POST("/runs", func(c *gin.Context) {
		var in struct {
			Prompt string `json:"prompt"`
		}
		if !depBind(c, &in) {
			return
		}
		p, _ := deploymentPrincipal(c)
		r, e := TeamService.Start(c, p.UserID, p.TenantID, in.Prompt)
		if e != nil {
			if errors.Is(e, team.ErrBusy) {
				c.JSON(409, gin.H{"error": e.Error()})
			} else {
				c.JSON(400, gin.H{"error": "invalid prompt or runtime unavailable"})
			}
			return
		}
		c.JSON(202, r)
	})
	g.GET("/runs/:id", func(c *gin.Context) {
		p, _ := deploymentPrincipal(c)
		r, e := TeamService.Get(c, c.Param("id"), p.UserID, p.TenantID, DeploymentService.Can(c, p, "team:run"))
		if e != nil {
			if errors.Is(e, mongo.ErrNoDocuments) {
				c.JSON(404, gin.H{"error": "run not found"})
			} else {
				c.JSON(503, gin.H{"error": "team storage unavailable"})
			}
			return
		}
		c.JSON(200, r)
	})
	g.POST("/runs/:id/cancel", func(c *gin.Context) {
		p, _ := deploymentPrincipal(c)
		_, e := TeamService.Get(c, c.Param("id"), p.UserID, p.TenantID, DeploymentService.Can(c, p, "team:run"))
		if e != nil {
			c.JSON(404, gin.H{"error": "run not found"})
			return
		}
		if e = TeamService.Cancel(c.Param("id")); e != nil {
			c.JSON(409, gin.H{"error": "run is not active"})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
}
