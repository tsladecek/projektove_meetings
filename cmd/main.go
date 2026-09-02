package main

import (
	"context"
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

	db, err := projektovemeeting.NewDB(config.DB)
	if err != nil {
		return fmt.Errorf("when constructing db adapter: %w", err)
	}

	projektove, err := projektovemeeting.NewProjektoveAPI(config.Projektove.URL, config.Projektove.Token, client, db)
	if err != nil {
		return fmt.Errorf("when constructing projektove adapter: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	controller := projektovemeeting.Controller{DB: db, Projektove: projektove, LLM: llm}

	if err := db.AddContext(ctx, projektovemeeting.LLMContextCreate{Name: "c1", Context: config.LLM.Context}); err != nil {
		return fmt.Errorf("when creating context: %w", err)
	}

	issues, err := controller.Infer(ctx, 1, string(meeting), config.Projektove.Users)
	if err != nil {
		return fmt.Errorf("when running issues inference: %w", err)
	}

	fmt.Printf("ISSUES:\n%+v", issues)
	return nil
}
