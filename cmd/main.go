package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	projektovemeeting "github.com/tsladecek/projektove_meeting"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	c := flag.String("c", "", "config path")
	v := flag.Bool("v", false, "print version and exit")
	flag.Parse()

	if *v {
		println(version)
		return nil
	}

	config, err := projektovemeeting.NewConfig(*c)
	if err != nil {
		return fmt.Errorf("when constructing config: %w", err)
	}

	client := projektovemeeting.NewClient()

	txp, repo, err := projektovemeeting.NewRepository(config.DB)
	if err != nil {
		return fmt.Errorf("when constructing data repository adapter: %w", err)
	}

	projektove, err := projektovemeeting.NewProjektoveAPI(config.Projektove.URL, client, repo)
	if err != nil {
		return fmt.Errorf("when constructing projektove adapter: %w", err)
	}

	controller := projektovemeeting.Controller{Repository: repo, Projektove: projektove, NewLLMProvider: projektovemeeting.NewLLM, Users: config.Projektove.Users, TxProvider: txp}

	queue := projektovemeeting.NewInferenceQueue(repo, projektovemeeting.DefaultQueueWorkers, projektovemeeting.DefaultQueuePollInterval, controller.RunInference)
	queue.Start()
	defer queue.Stop()

	auth, err := projektovemeeting.NewAuth(repo, config.OIDC, config.BaseURL)
	if err != nil {
		return fmt.Errorf("when constructing auth adapter: %w", err)
	}

	handler := projektovemeeting.NewHandler(auth, "/", config.OIDC.IDTokenCookieName, controller, config.Projektove.IssueEndpoint, config.OIDC.LogoutEndpoint)

	server := http.Server{Addr: fmt.Sprintf(":%d", config.Port), Handler: handler}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() {
		slog.Info("Starting server...", slog.Int("port", config.Port))
		if err := server.ListenAndServe(); err != nil {
			slog.Error(err.Error())
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	go func() {
		slog.Info("Shutting down server...")
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("when shutting down server", "err", err.Error())
		}

	}()

	return nil
}
