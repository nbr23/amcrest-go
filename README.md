# amcrest-go

## Running

### Go

Build with `go build` and run the executable, or run directly with `go run main.go`. Make sure to set the required envrionment variables defined below.

### Docker

A docker image is available on dockerhub: [nbr23/amcrest-go](https://hub.docker.com/r/nbr23/amcrest-go)

You can launch it with:

`docker run nbr23/amcrest-go`

Make sure to pass the required environment variables with `--env` or `--env-file`.

## Parameters

Parameters are passed through environment variables. The following are available:

```
# Connection variables
AMCREST_BASEURL=http://X.X.X.X # Base url for your Amcrest device. Mandatory
AMCREST_USER=admin # Defaults to "admin" if not set
AMCREST_PASSWORD= # Mandatory

AMCREST_TIMEZONE=America/New_York # Defaults to UTC if not set

# Telegram notification parameters
TELEGRAM_BOT_KEY= # Key of the bot to leverage. Mandatory for telegram notifications
TELEGRAM_CHAT_ID= # Id of the chat to post to. Mandatory for telegram notifications
```