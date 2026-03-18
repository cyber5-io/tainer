package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	registryBase = "https://ghcr.io/v2"
	org          = "cyber5-io"
)

type tagList struct {
	Tags []string `json:"tags"`
}

// FetchTags queries the ghcr.io OCI registry for available tags of a given image.
// Returns sorted version tags (filters out non-version tags like "latest").
func FetchTags(image string) ([]string, error) {
	fullImage := "tainer-" + image
	url := fmt.Sprintf("%s/%s/%s/tags/list", registryBase, org, fullImage)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("querying registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		// Try with token auth — ghcr.io requires a token even for public images
		token, err := getAnonymousToken(fullImage)
		if err != nil {
			return nil, fmt.Errorf("getting registry token: %w", err)
		}
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err = client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("querying registry with token: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
		}
	}

	var tl tagList
	if err := json.NewDecoder(resp.Body).Decode(&tl); err != nil {
		return nil, fmt.Errorf("decoding tags: %w", err)
	}

	// Filter to version-like tags only (e.g., "8.1", "8.2", "22", "7.4")
	var versions []string
	for _, tag := range tl.Tags {
		if tag == "latest" || strings.Contains(tag, "-") {
			continue
		}
		// Must start with a digit
		if len(tag) > 0 && tag[0] >= '0' && tag[0] <= '9' {
			versions = append(versions, tag)
		}
	}
	sort.Strings(versions)
	return versions, nil
}

// LocalTags queries locally cached images for available version tags of a given image name.
// For example, LocalTags("phpfpm") returns tags from cached "ghcr.io/cyber5-io/tainer-phpfpm:*" images.
func LocalTags(image string) []string {
	prefix := fmt.Sprintf("ghcr.io/%s/tainer-%s:", org, image)
	cmd := exec.Command("tainer", "images", "--format", "{{.Repository}}:{{.Tag}}", "--noheading")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		tag := strings.TrimPrefix(line, prefix)
		if tag == "latest" || tag == "<none>" || strings.Contains(tag, "-") {
			continue
		}
		if len(tag) > 0 && tag[0] >= '0' && tag[0] <= '9' {
			seen[tag] = true
		}
	}

	var tags []string
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// ImageExistsLocally returns true if the given image reference is available in local storage.
func ImageExistsLocally(image string) bool {
	return exec.Command("tainer", "image", "exists", image).Run() == nil
}

func getAnonymousToken(image string) (string, error) {
	scope := fmt.Sprintf("repository:%s/%s:pull", org, image)
	url := fmt.Sprintf("https://ghcr.io/token?scope=%s&service=ghcr.io", scope)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Token, nil
}
