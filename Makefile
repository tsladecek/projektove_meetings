dev:
	go run ./cmd -c ./config_dev.toml

tw:
	tailwindcss -i ./static/css/input.css -o ./static/css/output.css -w

