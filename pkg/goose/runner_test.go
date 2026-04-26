package goose

import (
	"net/http"
	"google.golang.org/adk/runner"
)

func _() {
	var _ http.Handler = (*runner.Runner)(nil)
}
