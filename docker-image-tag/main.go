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
	"time"
)

type harborClient struct {
	base    *url.URL
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
	Tags     []struct{ Name string `json:"name"` } `json:"tags"`
	PushTime time.Time                     `json:"push_time"`
}

func newHarborClient() *harborClient {
	raw := os.Getenv("HARBOR_URL")
	user := os.Getenv("HARBOR_USER")
	token := os.Getenv("HARBOR_TOKEN")
	if raw == "" || user == "" || token == "" {
		log.Fatalf("HARBOR_URL, HARBOR_USER and HARBOR_TOKEN must be set")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		log.Fatalf("invalid HARBOR_URL: %v", err)
	}
	return &harborClient{
		base:   parsed,
		user:   user,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// newRequest 构建一个带保留原始路径编码的请求
func (h *harborClient) newRequest(method, rawPath, query string) (*http.Request, error) {
	// rawPath 应包含前导 '/'
	u := &url.URL{
		Scheme: h.base.Scheme,
		Host:   h.base.Host,
		Opaque: rawPath,
		RawQuery: query,
	}
	req := &http.Request{
		Method: method,
		URL:    u,
	}
	req.SetBasicAuth(h.user, h.token)
	return req, nil
}

func (h *harborClient) imageExists(project, repo string) (bool, error) {
	// 手动将 repo 的 '/' 转为 '%2F'
	repoEsc := url.PathEscape(repo)
	path := fmt.Sprintf("/api/v2.0/projects/%s/repositories/%s/artifacts", project, repoEsc)
	req, err := h.newRequest("GET", path, "page=1&page_size=1")
	if err != nil {
		return false, err
	}
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
	repoEsc := url.PathEscape(repo)
	path := fmt.Sprintf("/api/v2.0/projects/%s/repositories/%s/artifacts", project, repoEsc)
	req, err := h.newRequest("GET", path, "page=1&page_size=100&with_tag=true")
	if err != nil {
		return "", err
	}
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
	var commitTags []struct{ Tag string; Time time.Time }
	commitRegex := regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	for _, art := range arts {
		for _, t := range art.Tags {
			if t.Name == "latest" {
				latestExists = true
			} else if commitRegex.MatchString(t.Name) {
				commitTags = append(commitTags, struct{ Tag string; Time time.Time }{t.Name, art.PushTime})
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
	repoEsc := url.PathEscape(repo)
	tagEsc := url.PathEscape(tag)
	path := fmt.Sprintf("/api/v2.0/projects/%s/repositories/%s/artifacts/%s", project, repoEsc, tagEsc)
	req, err := h.newRequest("GET", path, "")
	if err != nil {
		return false, err
	}
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
				json.NewEncoder(w).Encode	tagResponse{project, repo, tag})
			} else {
				json.NewEncoder(w).Encode	tagResponse{project, repo, "tag-not-exist"})
			}
			return
		}

		json.NewEncoder(w).Encode(tagResponse{project, repo, "tag-not-exist"})
	}
}

func main() {
	client := newHarborClient()
	http.HandleFunc("/", handler(client))
	log.Println("Starting server on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
