package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
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

	projektove, err := projektovemeeting.NewProjektoveAPI(client)
	if err != nil {
		return fmt.Errorf("when constructing projektove adapter: %w", err)
	}

	controller := projektovemeeting.Controller{Repository: repo, Projektove: projektove, NewLLMProvider: projektovemeeting.NewLLM, TxProvider: txp}

	if config.Auth.DefaultUser != "" {
		_, err := repo.GetUser(context.Background(), config.Auth.DefaultUser)
		if err != nil {
			hash, err := projektovemeeting.HashPassword(config.Auth.DefaultPassword)
			if err != nil {
				return fmt.Errorf("when hashing default user password: %w", err)
			}
			obj := projektovemeeting.UserCreate{
				Email:        config.Auth.DefaultUser,
				PasswordHash: new(hash),
				IsAdmin:      true,
			}
			if _, err := repo.StoreUser(context.Background(), obj); err != nil {
				return fmt.Errorf("when bootstrapping default user: %w", err)
			}
		}
	}

	queue := projektovemeeting.NewInferenceQueue(repo, projektovemeeting.DefaultQueueWorkers, projektovemeeting.DefaultQueuePollInterval, controller.RunInference)
	queue.Start()
	defer queue.Stop()

	var oidcConfig *projektovemeeting.ConfigOIDC
	if config.OIDC.Issuer != "" {
		oidcConfig = &config.OIDC
	}

	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		return fmt.Errorf("when parsing baseURL %q: %w", config.BaseURL, err)
	}

	endpointLogin := projektovemeeting.NewEndpoint(http.MethodGet, baseURL.Path, config.Auth.EndpointLogin)
	endpointLogout := projektovemeeting.NewEndpoint(http.MethodGet, baseURL.Path, config.Auth.EndpointLogout)

	auth, err := projektovemeeting.NewAuth(repo, oidcConfig, config.Auth.SecretKey, endpointLogin, endpointLogout, baseURL)
	if err != nil {
		return fmt.Errorf("when constructing auth adapter: %w", err)
	}

	handler := projektovemeeting.NewHandler(auth, "/", controller, endpointLogout)

	port := 80

	if config.TLS.CertPath != "" {
		port = 443
	}

	if baseURL.Port() != "" {
		port, err = strconv.Atoi(baseURL.Port())
		if err != nil {
			return fmt.Errorf("when parsing port from baseURL %q: %w", baseURL.String(), err)
		}
	}

	server := http.Server{Addr: fmt.Sprintf(":%d", port), Handler: handler}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() {
		slog.Info("Starting server...", slog.Int("port", port))
		if config.TLS.CertPath != "" {
			if err := server.ListenAndServeTLS(config.TLS.CertPath, config.TLS.KeyPath); err != nil {
				slog.Error(err.Error())
			}
		} else {
			if err := server.ListenAndServe(); err != nil {
				slog.Error(err.Error())
			}
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
