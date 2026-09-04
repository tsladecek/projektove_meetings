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
	BaseURL    string           `toml:"base_url" env:"BASEURL" env-required:"true"`
	Port       int              `toml:"port" env:"PORT" env-default:"8000"`
	Projektove ConfigProjektove `toml:"projektove" env-prefix:"PROJEKTOVE_" env-required:"true"`
	OIDC       ConfigOIDC       `toml:"oidc" env-prefix:"OIDC_"`
	LLM        ConfigLLM        `toml:"llm" env-prefix:"LLM_" env-required:"true"`
	Logging    ConfigLogging    `toml:"logging" env-prefix:"LOGGING_"`
}

type ProjektoveUsers []ProjektoveUser

type ConfigLogging struct {
	Level string `toml:"level" env:"LEVEL" env-default:"info"`
}

type ConfigOIDC struct {
	Issuer            string `toml:"issuer" env:"ISSUER" env-required:"true"`
	ClientID          string `toml:"client_id" env:"CLIENTID" env-required:"true"`
	ClientSecret      string `toml:"client_secret" env:"CLIENTSECRET" env-required:"true"`
	IDTokenCookieName string `toml:"id_token_cookie_name" env:"IDTOKENCOOKIENAME" env-required:"true"`
	CallbackEndpoint  string `toml:"callback_endpoint" env:"CALLBACKENDPOINT" env-default:"/oauth2/callback"`
	LogoutEndpoint    string `toml:"logout_endpoint" env:"LOGOUTENDPOINT" env-default:"/oauth2/logout"`
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
