package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var (
	harborURL      string
	harborUser     string
	harborPassword string
	httpClient     = &http.Client{}
	// Git commit regex: 7 to 40 hex characters
	commitRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
)

type response struct {
	Project string `json:"project"`
	Image   string `json:"image"`
	Tag     string `json:"tag"`
}

type harborTag struct {
	Name string `json:"name"`
}

func init() {
	harborURL = strings.TrimRight(os.Getenv("HARBOR_URL"), "/")
	harborUser = os.Getenv("HARBOR_USERNAME")
	harborPassword = os.Getenv("HARBOR_TOKEN")
	if harborURL == "" || harborUser == "" || harborPassword == "" {
		log.Fatal("Environment variables HARBOR_URL, HARBOR_USERNAME and HARBOR_TOKEN must be set")
	}
}

func main() {
	http.HandleFunc("/query", queryHandler)
	addr := ":8080"
	log.Printf("Starting server at %s...", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func queryHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query params
	project := r.URL.Query().Get("project")
	image := r.URL.Query().Get("image")
	tag := r.URL.Query().Get("tag")
	if project == "" || image == "" || tag == "" {
		http.Error(w, "Missing project, image or tag parameter", http.StatusBadRequest)
		return
	}

	// Escape path segments to handle special characters
	escProject := url.PathEscape(project)
	escImage := url.PathEscape(image)

	// Check if image exists by fetching at least one tag
	listURL := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/tags?page_size=1&page=1", harborURL, escProject, escImage)
	tags, err := callHarborList(listURL)
	if err != nil {
		respondJSON(w, response{Project: project, Image: image, Tag: "image-not-exist"})
		return
	}

	// If tag == "latest", return the name of the first (latest) tag
	if tag == "latest" {
		if len(tags) > 0 {
			respondJSON(w, response{Project: project, Image: image, Tag: tags[0].Name})
		} else {
			respondJSON(w, response{Project: project, Image: image, Tag: "no-tags-found"})
		}
		return
	}

	// If tag matches Git commit SHA
	if commitRe.MatchString(tag) {
		specURL := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/tags/%s", harborURL, escProject, escImage, url.PathEscape(tag))
		exists, err := callHarborExist(specURL)
		if err != nil || !exists {
			respondJSON(w, response{Project: project, Image: image, Tag: "tag-not-exist"})
		} else {
			respondJSON(w, response{Project: project, Image: image, Tag: tag})
		}
		return
	}

	// Fallback: return provided tag
	respondJSON(w, response{Project: project, Image: image, Tag: tag})
}

// callHarborList fetches list of tags, returns error if HTTP status >=400
func callHarborList(fullURL string) ([]harborTag, error) {
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(harborUser, harborPassword)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository not found")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("error status: %d", resp.StatusCode)
	}

	var tags []harborTag
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}
	return tags, nil
}

// callHarborExist checks if specific tag exists (HTTP 200)
func callHarborExist(fullURL string) (bool, error) {
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(harborUser, harborPassword)

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("unexpected status: %d", resp.StatusCode)
}

// respondJSON writes JSON response
func respondJSON(w http.ResponseWriter, data response) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
