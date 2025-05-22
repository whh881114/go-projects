package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"
)

type harborClient struct {
	baseURL string
	user    string
	token   string
	client  *http.Client
}

type tagResponse struct {
	Project string `json:"project"`
	Image   string `json:"image"`
	Tag     string `json:"tag"`
}

type artifact struct {
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

func doubleEncode(s string) string {
	first := url.PathEscape(s)      // 第一次编码，比如 library/nginx -> library%2Fnginx
	second := url.PathEscape(first) // 第二次编码，比如 library%2Fnginx -> library%252Fnginx
	return second
}

func newHarborClient() *harborClient {
	baseUrl := os.Getenv("HARBOR_URL")
	user := os.Getenv("HARBOR_USER")
	token := os.Getenv("HARBOR_TOKEN")
	if baseUrl == "" || user == "" || token == "" {
		log.Fatalf("HARBOR_URL, HARBOR_USER and HARBOR_TOKEN must be set")
	}
	return &harborClient{
		baseURL: baseUrl,
		user:    user,
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// check if image exists by listing artifacts
func (h *harborClient) imageExists(project, repo string) (bool, error) {
	apiUrl := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/artifacts?page=1&page_size=1", h.baseURL, project, doubleEncode(repo))
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(h.user, h.token)
	resp, err := h.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	var arts []artifact
	if err := json.NewDecoder(resp.Body).Decode(&arts); err != nil {
		return false, err
	}
	return len(arts) > 0, nil
}

// get latest tag by sorting artifacts by push_time descending
func (h *harborClient) getLatestTag(project, repo string) (string, error) {
	apiUrl := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/artifacts?page=1&page_size=1&sort=pushed_time:desc&with_tag=true", h.baseURL, project, doubleEncode(repo))
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(h.user, h.token)
	resp, err := h.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	var arts []artifact
	if err := json.NewDecoder(resp.Body).Decode(&arts); err != nil {
		return "", err
	}
	if len(arts) == 0 || len(arts[0].Tags) == 0 {
		return "", fmt.Errorf("no tags found")
	}
	return arts[0].Tags[0].Name, nil
}

// check if a specific tag exists
func (h *harborClient) tagExists(project, repo, tag string) (bool, error) {
	apiUrl := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s", h.baseURL, project, doubleEncode(repo), tag)
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(h.user, h.token)
	resp, err := h.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return true, nil
}

func handler(h *harborClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project := r.URL.Query().Get("project")
		repo := r.URL.Query().Get("image")
		tag := r.URL.Query().Get("tag")
		if project == "" || repo == "" || tag == "" {
			http.Error(w, "project, image and tag are required", http.StatusBadRequest)
			return
		}

		// check image existence
		exists, err := h.imageExists(project, repo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			json.NewEncoder(w).Encode(tagResponse{project, repo, "image-not-exist"})
			return
		}

		// latest tag
		if tag == "latest" {
			latest, err := h.getLatestTag(project, repo)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(tagResponse{project, repo, latest})
			return
		}

		// git commit pattern
		commitRegex := regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
		if commitRegex.MatchString(tag) {
			exists, err := h.tagExists(project, repo, tag)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if exists {
				json.NewEncoder(w).Encode(tagResponse{project, repo, tag})
			} else {
				json.NewEncoder(w).Encode(tagResponse{project, repo, "tag-not-exist"})
			}
			return
		}

		// default: tag format invalid or not found
		json.NewEncoder(w).Encode(tagResponse{project, repo, "tag-not-exist"})
	}
}

func main() {
	client := newHarborClient()
	http.HandleFunc("/", handler(client))
	log.Println("Starting server on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
