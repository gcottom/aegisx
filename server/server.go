package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gcottom/aegisx/config"
	"github.com/gcottom/aegisx/dynamicroutes"
	"github.com/gcottom/aegisx/handlers"
	"github.com/gcottom/aegisx/services"
	"github.com/gcottom/aegisx/util"
	"github.com/gcottom/qgin/qgin"
	"gopkg.in/tylerb/graceful.v1"
)

func Run() error {
	log.Println("Starting server")
	ctx := context.Background()
	log.Println("Loading config")
	cfg, err := config.LoadConfig(filepath.Join(util.GetAppRoot(), "config", "config.yaml"))
	if err != nil {
		log.Fatal("Failed to load config: ", err)
		return err
	}
	log.Println("Config loaded successfully")
	log.Println("Creating GPT client")
	gptClient := util.NewGPTClient(cfg.GptApiKey)
	if gptClient == nil {
		log.Fatal("Failed to create GPT client")
		return errors.New("failed to create GPT client")
	}
	log.Println("GPT client created successfully")
	executorService := &services.ExecuterService{
		GPTClient:  gptClient,
		RetryLimit: 5,
	}
	log.Println("Creating executor service")
	router := qgin.NewGinEngine(&ctx, &qgin.Config{LogRequestID: true, ProdMode: true})
	mainHandler := &handlers.MainHandler{
		ExecutorService: executorService,
	}
	routerSwitcher := util.NewRouterSwitcher(router)
	dynamicroutes.CreateRoutes(router, mainHandler)
	log.Println("Creating routes")
	dynamicRouteService := &dynamicroutes.DynamicRouteService{
		Handler:        mainHandler,
		Router:         router,
		RouterSwitcher: routerSwitcher,
	}
	executorService.DynamicRouteService = dynamicRouteService
	log.Println("Starting server")
	log.Printf("Server listening on port %d\n", cfg.Port)
	server := CreateGracefulServer(routerSwitcher, cfg.Port)
	return server.ListenAndServe()

}

func CreateGracefulServer(router *util.RouterSwitcher, port int) *graceful.Server {
	return &graceful.Server{
		Server: &http.Server{
			Addr:         ":" + strconv.Itoa(port),
			Handler:      router,
			ReadTimeout:  1 * time.Minute,
			WriteTimeout: 5 * time.Minute,
			IdleTimeout:  3 * time.Minute,
		},
		Timeout: 30 * time.Second,
	}
}
