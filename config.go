package projektovemeeting

import (
	"fmt"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Projektove ConfigProjektove `toml:"projektove" env-prefix:"PROJEKTOVE_" env-required:"true"`
	LLM        ConfigLLM        `toml:"llm" env-prefix:"LLM_" env-required:"true"`
}

type ProjektoveUsers []ProjektoveUser

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

	return cfg, nil
}
