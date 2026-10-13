// Local mock demo using the production login, authorization, API and React build.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/free5gc/util/mongoapi"
	"github.com/free5gc/webconsole/backend/WebUI"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	uri := flag.String("mongo-uri", "mongodb://127.0.0.1:27019", "isolated Mongo URI")
	db := flag.String("database", "rbac_mock_demo", "isolated demo DB")
	addr := flag.String("addr", "127.0.0.1:5080", "local HTTP address")
	frontend := flag.String("frontend", "frontend/build", "built React UI")
	flag.Parse()
	if os.Getenv("FREE5GC_DEPLOYMENT_MODE") != "mock" {
		panic("demo requires explicit mock mode")
	}
	if err := mongoapi.SetMongoDB(*db, *uri); err != nil {
		panic(err)
	}
	if err := WebUI.InitDeployment(context.Background(), *db); err != nil {
		panic(err)
	}
	WebUI.SetAdmin()
	if err := WebUI.InitTeam(context.Background(), *db); err != nil {
		panic(err)
	}
	if err := WebUI.InitJwtKey(); err != nil {
		panic(err)
	}
	router := WebUI.NewRouter()
	router.Static("/assets", filepath.Join(*frontend, "assets"))
	router.NoRoute(func(c *gin.Context) {
		if len(c.Request.URL.Path) >= 5 && c.Request.URL.Path[:5] == "/api/" {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}
		c.File(filepath.Join(*frontend, "index.html"))
	})
	WebUI.DeploymentService.StepDelay = 2 * time.Second
	go WebUI.DeploymentService.Run(context.Background())
	fmt.Printf("Local MOCK demo: http://%s/deployments (no privileged worker)\n", *addr)
	shutdown, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: *addr, Handler: router}
	go func() {
		<-shutdown.Done()
		if WebUI.TeamService != nil {
			WebUI.TeamService.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}
