package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/innomon/adk-cloud-proxy/pkg/auth"
)

func main() {
	routerProxyURL := os.Getenv("ROUTER_PROXY_URL")
	if routerProxyURL == "" {
		routerProxyURL = "http://localhost:8080"
	}

	nkeySeed := os.Getenv("NKEY_SEED")
	if nkeySeed == "" {
		log.Fatal("NKEY_SEED environment variable is required")
	}

	userID := os.Getenv("USER_ID")
	if userID == "" {
		log.Fatal("USER_ID environment variable is required")
	}

	appID := os.Getenv("APP_ID")
	if appID == "" {
		log.Fatal("APP_ID environment variable is required")
	}

	token, err := auth.GenerateToken([]byte(nkeySeed), userID, appID, "", 1*time.Hour)
	if err != nil {
		log.Fatalf("Failed to generate JWT: %v", err)
	}

	body := `{"message": "Hello from chatbot"}`
	req, err := http.NewRequest(http.MethodPost, routerProxyURL, strings.NewReader(body))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}

	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Body:   %s\n", respBody)
}
