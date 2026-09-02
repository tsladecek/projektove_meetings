package projektovemeeting

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	DB         string           `toml:"db" env:"DB" description:"name of the sqlite database for storing cache and prompt history" env-default:"db.sqlite"`
	Projektove ConfigProjektove `toml:"projektove" env-prefix:"PROJEKTOVE_" env-required:"true"`
	LLM        ConfigLLM        `toml:"llm" env-prefix:"LLM_" env-required:"true"`
	Logging    ConfigLogging    `toml:"logging" env-prefix:"LOGGING_"`
}

type ProjektoveUsers []ProjektoveUser

type ConfigLogging struct {
	Level string `toml:"level" env:"LEVEL" env-default:"info"`
}

type ConfigProjektove struct {
	URL   string          `toml:"url" env:"URL" env-required:"true"`
	Token string          `toml:"token" env:"TOKEN" env-required:"true"`
	Users ProjektoveUsers `toml:"users" env:"USERS" env-required:"true"`
}

type ConfigLLM struct {
	Provider string `toml:"provider" env:"PROVIDER" env-required:"true"`
	Model    string `toml:"model" env:"MODEL"`
	Token    string `toml:"token" env:"TOKEN"`
	Context  string `toml:"context" env:"CONTEXT" env-required:"true"`
}

func NewConfig(configPath string) (Config, error) {
	var cfg Config

	if configPath != "" {
		if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
			return Config{}, fmt.Errorf("when reading config: %w", err)
		}
	}
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return Config{}, fmt.Errorf("when reading env vars: %w", err)
	}

	var level slog.Level
	switch strings.ToLower(cfg.Logging.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return Config{}, fmt.Errorf("unrecognized logger level %q", cfg.Logging.Level)
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))

	return cfg, nil
}
