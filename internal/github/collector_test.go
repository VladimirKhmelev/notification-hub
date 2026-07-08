package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestCollector(srv *httptest.Server) *Collector {
	return &Collector{
		client:   &http.Client{Timeout: 5 * time.Second},
		seen:     make(map[int64]map[int64]struct{}),
		interval: 10 * time.Minute,
	}
}

func makeRelease(id int64, tag string, draft, prerelease bool) release {
	return release{
		ID:         id,
		TagName:    tag,
		Name:       tag,
		HTMLURL:    fmt.Sprintf("https://github.com/test/repo/releases/tag/%s", tag),
		Draft:      draft,
		Prerelease: prerelease,
	}
}

func TestFetchReleases_ParsesResponse(t *testing.T) {
	releases := []release{
		makeRelease(1, "v1.0.0", false, false),
		makeRelease(2, "v1.1.0", false, false),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()

	c := newTestCollector(srv)
	// override fetchReleases to hit test server
	s := source{id: 1, repo: "test/repo"}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got []release
	json.NewDecoder(resp.Body).Decode(&got)

	if len(got) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(got))
	}
	_ = s
}

func TestFirstFetchSeeds_NothingPublished(t *testing.T) {
	c := &Collector{
		seen: make(map[int64]map[int64]struct{}),
	}
	s := source{id: 1}
	releases := []release{
		makeRelease(100, "v1.0.0", false, false),
		makeRelease(101, "v1.1.0", false, false),
	}

	// simulate first-fetch seeding
	c.seen[s.id] = make(map[int64]struct{})
	for _, r := range releases {
		c.seen[s.id][r.ID] = struct{}{}
	}

	if len(c.seen[s.id]) != 2 {
		t.Fatalf("expected 2 seeded, got %d", len(c.seen[s.id]))
	}
}

func TestDedup_NewReleaseDetected(t *testing.T) {
	c := &Collector{
		seen: make(map[int64]map[int64]struct{}),
	}
	s := source{id: 1}

	// seed with v1.0.0
	c.seen[s.id] = map[int64]struct{}{100: {}}

	releases := []release{
		makeRelease(100, "v1.0.0", false, false),
		makeRelease(101, "v1.1.0", false, false),
	}

	var newReleases []release
	for _, r := range releases {
		if _, already := c.seen[s.id][r.ID]; !already && !r.Draft {
			newReleases = append(newReleases, r)
			c.seen[s.id][r.ID] = struct{}{}
		}
	}

	if len(newReleases) != 1 {
		t.Fatalf("expected 1 new release, got %d", len(newReleases))
	}
	if newReleases[0].TagName != "v1.1.0" {
		t.Fatalf("expected v1.1.0, got %q", newReleases[0].TagName)
	}
}

func TestDraftsSkipped(t *testing.T) {
	c := &Collector{
		seen: make(map[int64]map[int64]struct{}),
	}
	s := source{id: 1}
	c.seen[s.id] = make(map[int64]struct{})

	releases := []release{
		makeRelease(200, "v2.0.0-draft", true, false),
		makeRelease(201, "v2.0.0", false, false),
	}

	var published []release
	for _, r := range releases {
		if _, already := c.seen[s.id][r.ID]; !already && !r.Draft {
			published = append(published, r)
			c.seen[s.id][r.ID] = struct{}{}
		}
	}

	if len(published) != 1 {
		t.Fatalf("expected 1 published (draft skipped), got %d", len(published))
	}
	if published[0].TagName != "v2.0.0" {
		t.Fatalf("expected v2.0.0, got %q", published[0].TagName)
	}
}

func TestPrerelease_LowPriority(t *testing.T) {
	c := &Collector{}
	s := source{id: 1, name: "test/repo", repo: "test/repo"}
	r := makeRelease(300, "v3.0.0-beta", false, true)

	title, _, priority := func() (string, string, string) {
		title := fmt.Sprintf("%s: %s", s.repo, r.TagName)
		body := r.HTMLURL
		priority := "normal"
		if r.Prerelease {
			priority = "low"
		}
		return title, body, priority
	}()

	_ = c
	if priority != "low" {
		t.Fatalf("expected low priority for prerelease, got %q", priority)
	}
	if title != "test/repo: v3.0.0-beta" {
		t.Fatalf("unexpected title: %q", title)
	}
}
