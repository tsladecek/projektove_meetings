package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
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
	m := flag.String("m", "", "meeting notes")
	flag.Parse()

	config, err := projektovemeeting.NewConfig(*c)
	if err != nil {
		return fmt.Errorf("when constructing config: %w", err)
	}

	meeting, err := os.ReadFile(*m)
	if err != nil {
		return fmt.Errorf("when reading meeting notes")
	}

	llm, err := projektovemeeting.NewLLM(projektovemeeting.LLMProvider(config.LLM.Provider), config.LLM.Model, config.LLM.Token)
	if err != nil {
		return fmt.Errorf("when constructing llm adapter: %w", err)
	}

	client := projektovemeeting.NewClient()

	txp, repo, err := projektovemeeting.NewRepository(config.DB)
	if err != nil {
		return fmt.Errorf("when constructing data repository adapter: %w", err)
	}
	_ = txp

	userCreate := projektovemeeting.UserCreate{Email: "user@user.com", ProjektoveToken: config.Projektove.Token, Models: []projektovemeeting.LLMModel{{Provider: projektovemeeting.LLMProvider(config.LLM.Provider), Model: config.LLM.Model, Token: config.LLM.Token}}}
	repo.GetUser(context.Background(), "")

	user, err := repo.GetUser(context.Background(), userCreate.Email)
	if err != nil {
		if !errors.Is(err, projektovemeeting.ErrUserNotFound) {
			return fmt.Errorf("when fetching user: %w", err)
		}

		_, err = repo.StoreUser(context.Background(), userCreate)
		if err != nil {
			return fmt.Errorf("when storing user: %w", err)
		}

		user, err = repo.GetUser(context.Background(), userCreate.Email)
		if err != nil {
			return fmt.Errorf("when fetching user: %w", err)
		}
	}

	projektove, err := projektovemeeting.NewProjektoveAPI(config.Projektove.URL, config.Projektove.Token, client, repo)
	if err != nil {
		return fmt.Errorf("when constructing projektove adapter: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	controller := projektovemeeting.Controller{Repository: repo, Projektove: projektove, LLM: llm}

	if _, err := repo.StoreContext(ctx, user, projektovemeeting.LLMContextCreate{Name: "c1", Context: config.LLM.Context}); err != nil {
		return fmt.Errorf("when creating context: %w", err)
	}

	issues, err := controller.Infer(ctx, user, 1, string(meeting), config.Projektove.Users)
	if err != nil {
		return fmt.Errorf("when running issues inference: %w", err)
	}

	fmt.Printf("ISSUES:\n%+v", issues)
	return nil
}
