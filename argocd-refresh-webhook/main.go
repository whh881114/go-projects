package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

// RefreshRequest represents the incoming JSON payload
type RefreshRequest struct {
	Applications []string `json:"applications"`
}

// RefreshResponse represents the service response
type RefreshResponse struct {
	Refreshed []string `json:"refreshed"`
	Ignored   []string `json:"ignored"`
}

func main() {
	// Load configuration from environment variables
	server := os.Getenv("ARGOCD_SERVER")
	token := os.Getenv("ARGOCD_TOKEN")
	if server == "" || token == "" {
		log.Fatal("Missing required environment variables: ARGOCD_SERVER and ARGOCD_TOKEN must be set")
	}

	// HTTP handler for refresh endpoint
	http.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var req RefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		// Prepare HTTP client and headers
		httpClient := &http.Client{}

		var respData RefreshResponse

		// Iterate over applications to refresh
		for _, app := range req.Applications {
			url := fmt.Sprintf("%s/api/v1/applications/%s/refresh", server, app)
			// Build refresh payload (empty body triggers default behavior)
			body := map[string]interface{}{"force": true}
			bodyBytes, _ := json.Marshal(body)

			reqRefresh, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
			if err != nil {
				log.Printf("Error creating request for application %s: %v", app, err)
				respData.Ignored = append(respData.Ignored, app)
				continue
			}
			reqRefresh.Header.Set("Content-Type", "application/json")
			reqRefresh.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

			resp, err := httpClient.Do(reqRefresh)
			if err != nil {
				log.Printf("Error refreshing application %s: %v", app, err)
				respData.Ignored = append(respData.Ignored, app)
				continue
			}
			defer resp.Body.Close()

			// Read response body for logging
			respBody, _ := ioutil.ReadAll(resp.Body)

			if resp.StatusCode == http.StatusOK {
				respData.Refreshed = append(respData.Refreshed, app)
				log.Printf("Successfully refreshed application %s", app)
			} else if resp.StatusCode == http.StatusNotFound {
				respData.Ignored = append(respData.Ignored, app)
				log.Printf("Application not found %s: %s", app, string(respBody))
			} else {
				respData.Ignored = append(respData.Ignored, app)
				log.Printf("Failed to refresh application %s: %s", app, string(respBody))
			}
		}

		// Write response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respData)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := fmt.Sprintf(":%s", port)
	log.Printf("Starting ArgoCD refresh service on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
