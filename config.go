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
	BaseURL    string           `toml:"base_url" env:"BASEURL" env-default:"/"`
	Port       int              `toml:"port" env:"PORT" env-default:"8000"`
	Projektove ConfigProjektove `toml:"projektove" env-prefix:"PROJEKTOVE_" env-required:"true"`
	Auth       ConfigAuth       `toml:"auth" env-prefix:"AUTH_" env-required:"true"`
	OIDC       ConfigOIDC       `toml:"oidc" env-prefix:"OIDC_"`
	Logging    ConfigLogging    `toml:"logging" env-prefix:"LOGGING_"`
}

type ConfigLogging struct {
	Level string `toml:"level" env:"LEVEL" env-default:"info"`
}

type ConfigAuth struct {
	SecretKey       string `toml:"secret_key" env:"SECRETKEY" env-required:"true"`
	DefaultUser     string `toml:"default_user" env:"DEFAULTUSER" env-default:"admin"`
	DefaultPassword string `toml:"default_password" env:"DEFAULTPASSWORD" env-default:"password"`

	EndpointLogin  string `toml:"endpoint_login" env:"ENDPOINTLOGIN" env-default:"/login"`
	EndpointLogout string `toml:"endpoint_logout" env:"ENDPOINTLOGOUT" env-default:"/logout"`
}

type ConfigOIDC struct {
	Issuer                 string `toml:"issuer" env:"ISSUER"`
	ClientID               string `toml:"client_id" env:"CLIENTID"`
	ClientSecret           string `toml:"client_secret" env:"CLIENTSECRET"`
	EndSessionURL          string `toml:"end_session_url" env:"ENDSESSIONURL"`
	IDTokenCookieName      string `toml:"id_token_cookie_name" env:"IDTOKENCOOKIENAME" env-default:"id_token"`
	RefreshTokenCookieName string `toml:"refresh_token_cookie_name" env:"REFRESHTOKENCOOKIENAME" env-default:"refresh_token"`
	CallbackEndpoint       string `toml:"callback_endpoint" env:"CALLBACKENDPOINT" env-default:"/oauth2/callback"`
}

type ConfigProjektove struct {
	URL           string `toml:"url" env:"URL" env-required:"true" env-description:"api url"`
	IssueEndpoint string `toml:"issue_endpoint" env:"ISSUEENDPOINT" env-required:"true" env-description:"UI url to an issue, e.g. https://app.projektove.cz/<org>/tasks/%d"`
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
