package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	conf "github.com/ashwinsekaran/webpage-analysis-go/config"
	"github.com/ashwinsekaran/webpage-analysis-go/handlers"
	"github.com/julienschmidt/httprouter"
	"github.com/kelseyhightower/envconfig"
)

func main() {
	var config conf.Config
	envconfig.MustProcess("", &config)

	webHandler, err := handlers.NewWebAnalysisHandler(
		"templates",
		handlers.NewHTTPAnalyzer(
			time.Duration(config.RequestTimeoutSeconds)*time.Second,
			time.Duration(config.LinkCheckTimeoutSeconds)*time.Second,
			config.MaxCheckedLinks,
		),
	)
	if err != nil {
		log.Fatalf("initialize web handler: %v", err)
	}

	r := httprouter.New()
	r.Handle(handlers.Ok("/.well-known/ready"))
	r.Handle(handlers.Ok("/.well-known/live"))
	r.Handle(http.MethodGet, "/", webHandler.Get)
	r.Handle(http.MethodPost, "/", webHandler.Post)
	r.Handle(http.MethodPost, "/api/analyze", webHandler.AnalyzeAPI)
	r.ServeFiles("/static/*filepath", http.Dir("static"))

	server := http.Server{Addr: config.HttpListenAddress, Handler: r}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("starting server at %s", config.HttpListenAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("start server: %v", err)
		}
	}()

	<-shutdown
	log.Println("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown: %v", err)
	}
}
