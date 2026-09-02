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

	projektove, err := projektovemeeting.NewProjektoveAPI(config.Projektove.URL, config.Projektove.Token, client)
	if err != nil {
		return fmt.Errorf("when constructing projektove adapter: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return projektovemeeting.Infer(ctx, projektove, llm, config.LLM.Context, string(meeting), config.Projektove.Users)
}
