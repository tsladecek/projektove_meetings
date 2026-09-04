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

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	c := flag.String("c", "", "config path")
	flag.Parse()

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

	controller := projektovemeeting.Controller{Repository: repo, Projektove: projektove, NewLLMProvider: projektovemeeting.NewLLM, TxProvider: txp}

	auth, err := projektovemeeting.NewAuth(repo, config.OIDC, config.BaseURL)
	if err != nil {
		return fmt.Errorf("when constructing auth adapter: %w", err)
	}

	handler := projektovemeeting.NewHandler(auth, "/", config.OIDC.IDTokenCookieName, controller)

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

	// userCreate := projektovemeeting.UserCreate{Email: "user@user.com", ProjektoveToken: config.Projektove.Token, Models: []projektovemeeting.LLMModel{{Provider: projektovemeeting.LLMProvider(config.LLM.Provider), Model: config.LLM.Model, Token: config.LLM.Token}}}
	// repo.GetUser(context.Background(), "")
	//
	// user, err := repo.GetUser(context.Background(), userCreate.Email)
	// if err != nil {
	// 	if !errors.Is(err, projektovemeeting.ErrUserNotFound) {
	// 		return fmt.Errorf("when fetching user: %w", err)
	// 	}
	//
	// 	_, err = repo.StoreUser(context.Background(), userCreate)
	// 	if err != nil {
	// 		return fmt.Errorf("when storing user: %w", err)
	// 	}
	//
	// 	user, err = repo.GetUser(context.Background(), userCreate.Email)
	// 	if err != nil {
	// 		return fmt.Errorf("when fetching user: %w", err)
	// 	}
	// }
	//
	//
	// ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	// defer cancel()
	//
	// controller := projektovemeeting.Controller{Repository: repo, Projektove: projektove, LLM: llm, TxProvider: txp}
	//
	// if _, err := repo.StoreContext(ctx, user, projektovemeeting.LLMContextCreate{Name: "c1", Context: config.LLM.Context}); err != nil {
	// 	return fmt.Errorf("when creating context: %w", err)
	// }
	//
	// issues, err := controller.Infer(ctx, user, 1, string(meeting), config.Projektove.Users)
	// if err != nil {
	// 	return fmt.Errorf("when running issues inference: %w", err)
	// }
	//
	// fmt.Printf("ISSUES:\n%+v", issues)
	// return nil
}
