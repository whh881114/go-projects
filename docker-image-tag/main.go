package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
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
	PushTime time.Time `json:"push_time"`
}

func newHarborClient() *harborClient {
	url := os.Getenv("HARBOR_URL")
	user := os.Getenv("HARBOR_USER")
	token := os.Getenv("HARBOR_TOKEN")
	if url == "" || user == "" || token == "" {
		log.Fatalf("HARBOR_URL, HARBOR_USER and HARBOR_TOKEN must be set")
	}
	return &harborClient{
		baseURL: url,
		user:    user,
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// escapePath 转义 URL 路径段
func escapePath(parts ...string) string {
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// apiURL 构建带转义的 API 路径
func (h *harborClient) apiURL(pathSegments ...string) string {
	path := escapePath(pathSegments...)
	return fmt.Sprintf("%s/api/v2.0/%s", h.baseURL, path)
}

func (h *harborClient) imageExists(project, repo string) (bool, error) {
	endpoint := fmt.Sprintf("%s?%s", h.apiURL(
		"projects", project,
		"repositories", repo, "artifacts",
	), "page=1&page_size=1")
	req, err := http.NewRequest("GET", endpoint, nil)
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

func (h *harborClient) getBestTag(project, repo string) (string, error) {
	endpoint := fmt.Sprintf("%s?%s", h.apiURL(
		"projects", project,
		"repositories", repo, "artifacts",
	), "page=1&page_size=100&with_tag=true")
	req, err := http.NewRequest("GET", endpoint, nil)
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

	var latestExists bool
	var commitTags []struct {
		Tag  string
		Time time.Time
	}
	commitRegex := regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	for _, art := range arts {
		for _, t := range art.Tags {
			if t.Name == "latest" {
				latestExists = true
			} else if commitRegex.MatchString(t.Name) {
				commitTags = append(commitTags, struct {
					Tag  string
					Time time.Time
				}{t.Name, art.PushTime})
			}
		}
	}
	if latestExists {
		return "latest", nil
	}
	if len(commitTags) == 0 {
		return "", fmt.Errorf("no valid tags found")
	}
	sort.Slice(commitTags, func(i, j int) bool { return commitTags[i].Time.After(commitTags[j].Time) })
	return commitTags[0].Tag, nil
}

func (h *harborClient) tagExists(project, repo, tag string) (bool, error) {
	endpoint := h.apiURL("projects", project,
		"repositories", repo, "artifacts", tag)
	req, err := http.NewRequest("GET", endpoint, nil)
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

		exists, err := h.imageExists(project, repo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			json.NewEncoder(w).Encode(tagResponse{project, repo, "image-not-exist"})
			return
		}

		if tag == "latest" {
			bestTag, err := h.getBestTag(project, repo)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(tagResponse{project, repo, bestTag})
			return
		}

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

		// 默认：格式不匹配
		json.NewEncoder(w).Encode(tagResponse{project, repo, "tag-not-exist"})
	}
}

func main() {
	client := newHarborClient()
	http.HandleFunc("/", handler(client))
	log.Println("Starting server on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
