package dynamicroutes

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"

	"github.com/gcottom/aegisx/util"
	"github.com/gcottom/qgin/qgin"
	"github.com/gin-gonic/gin"
)

type DynamicRouteService struct {
	Handler        Handlers
	Router         *gin.Engine
	RouterSwitcher *util.RouterSwitcher
	ProxyMap       sync.Map
}

type Handlers interface {
	Execute(c *gin.Context)
	Stop(c *gin.Context)
	Status(c *gin.Context)
}

func CreateRoutes(router *gin.Engine, handler Handlers) {
	router.POST("/execute", handler.Execute)
	router.POST("/stop/:id", handler.Stop)
	router.GET("/status/:id", handler.Status)
}

func (s *DynamicRouteService) RegisterReverseProxy(runtimeID string, port int) {
	targetURL, _ := url.Parse("http://localhost:" + strconv.Itoa(port))
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("X-Application-Base", targetURL.RawPath+"/runtime/"+runtimeID)
		return nil
	}

	// Store the proxy in sync.Map
	s.ProxyMap.Store(runtimeID, proxy)

	// Register endpoint in Gin router
	s.Router.Any("/runtime/"+runtimeID+"/*any", func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	})

	log.Printf("✅ Proxy registered: /runtime/%s → localhost:%d", runtimeID, port)
}

func (s *DynamicRouteService) DeregisterReverseProxy(runtimeID string) {
	// Check if proxy exists
	_, exists := s.ProxyMap.Load(runtimeID)
	if !exists {
		log.Printf("⚠️ Proxy not found for runtime: %s", runtimeID)
		return
	}

	// Delete from sync.Map
	s.ProxyMap.Delete(runtimeID)
	ctx := context.Background()

	// Remove the dynamic route by replacing the router
	newRouter := qgin.NewGinEngine(&ctx, &qgin.Config{LogRequestID: true, ProdMode: true})
	CreateRoutes(newRouter, s.Handler)
	s.ProxyMap.Range(func(id, value interface{}) bool {
		proxy := value.(*httputil.ReverseProxy)
		newRouter.Any("/runtime/"+id.(string)+"/*any", func(c *gin.Context) {
			proxy.ServeHTTP(c.Writer, c.Request)
		})
		return true
	})

	// Replace router instance
	s.Router = newRouter
	s.RouterSwitcher.UpdateRouter(newRouter)

	log.Printf("❌ Proxy deregistered: /runtime/%s", runtimeID)
}
