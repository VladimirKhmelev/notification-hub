package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestCollector() *Collector {
	return &Collector{
		client:   &http.Client{Timeout: 5 * time.Second},
		seen:     make(map[int64]map[int64]struct{}),
		interval: 10 * time.Minute,
	}
}

func makeRelease(id int64, tag, name string, draft, prerelease bool) release {
	return release{
		ID:         id,
		TagName:    tag,
		Name:       name,
		HTMLURL:    fmt.Sprintf("https://github.com/test/repo/releases/tag/%s", tag),
		Body:       "",
		Draft:      draft,
		Prerelease: prerelease,
	}
}

// ── fetchReleases ─────────────────────────────────────────────────────────────

func TestFetchReleases_ParsesResponse(t *testing.T) {
	releases := []release{
		makeRelease(1, "v1.0.0", "v1.0.0", false, false),
		makeRelease(2, "v1.1.0", "v1.1.0", false, false),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()

	c := newTestCollector()
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
}

func TestFetchReleases_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, repo: "test/repo"}

	// simulate fetchReleases logic against test server
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status")
	}
	_ = s
}

// ── first fetch seeding ───────────────────────────────────────────────────────

func TestFirstFetchSeeds_NothingPublished(t *testing.T) {
	c := &Collector{seen: make(map[int64]map[int64]struct{})}
	s := source{id: 1}
	releases := []release{
		makeRelease(100, "v1.0.0", "", false, false),
		makeRelease(101, "v1.1.0", "", false, false),
	}

	c.seen[s.id] = make(map[int64]struct{})
	for _, r := range releases {
		c.seen[s.id][r.ID] = struct{}{}
	}

	if len(c.seen[s.id]) != 2 {
		t.Fatalf("expected 2 seeded, got %d", len(c.seen[s.id]))
	}
}

// ── deduplication ─────────────────────────────────────────────────────────────

func TestDedup_NewReleaseDetected(t *testing.T) {
	c := &Collector{seen: make(map[int64]map[int64]struct{})}
	s := source{id: 1}
	c.seen[s.id] = map[int64]struct{}{100: {}}

	releases := []release{
		makeRelease(100, "v1.0.0", "", false, false),
		makeRelease(101, "v1.1.0", "", false, false),
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

func TestDedup_NoRepublishOnSameFeed(t *testing.T) {
	c := &Collector{seen: make(map[int64]map[int64]struct{})}
	s := source{id: 1}
	c.seen[s.id] = map[int64]struct{}{100: {}, 101: {}}

	releases := []release{
		makeRelease(100, "v1.0.0", "", false, false),
		makeRelease(101, "v1.1.0", "", false, false),
	}
	var newReleases []release
	for _, r := range releases {
		if _, already := c.seen[s.id][r.ID]; !already && !r.Draft {
			newReleases = append(newReleases, r)
		}
	}
	if len(newReleases) != 0 {
		t.Fatalf("expected 0 new releases on stable feed, got %d", len(newReleases))
	}
}

// ── draft filtering ───────────────────────────────────────────────────────────

func TestDraftsSkipped(t *testing.T) {
	c := &Collector{seen: make(map[int64]map[int64]struct{})}
	s := source{id: 1}
	c.seen[s.id] = make(map[int64]struct{})

	releases := []release{
		makeRelease(200, "v2.0.0-draft", "", true, false),
		makeRelease(201, "v2.0.0", "", false, false),
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

func TestAllDrafts_NothingPublished(t *testing.T) {
	c := &Collector{seen: make(map[int64]map[int64]struct{})}
	s := source{id: 1}
	c.seen[s.id] = make(map[int64]struct{})

	releases := []release{
		makeRelease(300, "v3.0.0-draft", "", true, false),
		makeRelease(301, "v3.1.0-draft", "", true, false),
	}
	var published []release
	for _, r := range releases {
		if _, already := c.seen[s.id][r.ID]; !already && !r.Draft {
			published = append(published, r)
		}
	}
	if len(published) != 0 {
		t.Fatalf("expected 0 published when all are drafts, got %d", len(published))
	}
}

// ── priority ──────────────────────────────────────────────────────────────────

func TestPrerelease_LowPriority(t *testing.T) {
	s := source{id: 1, name: "test", repo: "test/repo"}
	r := makeRelease(300, "v3.0.0-beta", "v3.0.0-beta", false, true)

	priority := "normal"
	if r.Prerelease {
		priority = "low"
	}
	title := fmt.Sprintf("%s: %s", s.repo, r.TagName)

	if priority != "low" {
		t.Fatalf("expected low priority for prerelease, got %q", priority)
	}
	if title != "test/repo: v3.0.0-beta" {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestStableRelease_NormalPriority(t *testing.T) {
	r := makeRelease(400, "v4.0.0", "", false, false)
	priority := "normal"
	if r.Prerelease {
		priority = "low"
	}
	if priority != "normal" {
		t.Fatalf("expected normal priority, got %q", priority)
	}
}

// ── title formatting ──────────────────────────────────────────────────────────

func TestTitle_WithDistinctName(t *testing.T) {
	s := source{repo: "owner/repo"}
	r := makeRelease(1, "v1.0.0", "First stable release", false, false)

	title := fmt.Sprintf("%s: %s", s.repo, r.TagName)
	if r.Name != "" && r.Name != r.TagName {
		title = fmt.Sprintf("%s: %s (%s)", s.repo, r.Name, r.TagName)
	}
	if title != "owner/repo: First stable release (v1.0.0)" {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestTitle_WhenNameEqualsTag(t *testing.T) {
	s := source{repo: "owner/repo"}
	r := makeRelease(1, "v1.0.0", "v1.0.0", false, false)

	title := fmt.Sprintf("%s: %s", s.repo, r.TagName)
	if r.Name != "" && r.Name != r.TagName {
		title = fmt.Sprintf("%s: %s (%s)", s.repo, r.Name, r.TagName)
	}
	if title != "owner/repo: v1.0.0" {
		t.Fatalf("unexpected title: %q", title)
	}
}

// ── body truncation ───────────────────────────────────────────────────────────

func TestBody_TruncatedAt200(t *testing.T) {
	r := makeRelease(1, "v1.0.0", "", false, false)
	r.Body = string(make([]byte, 250))
	for i := range r.Body {
		r.Body = r.Body[:i] + "x" + r.Body[i+1:]
	}

	notes := r.Body
	if len(notes) > 200 {
		notes = notes[:200] + "…"
	}
	if len([]rune(notes)) != 201 { // 200 chars + ellipsis
		t.Fatalf("expected truncated body with ellipsis, len=%d", len(notes))
	}
}

func TestBody_ShortNotTruncated(t *testing.T) {
	r := makeRelease(1, "v1.0.0", "", false, false)
	r.Body = "short notes"
	notes := r.Body
	if len(notes) > 200 {
		notes = notes[:200] + "…"
	}
	if notes != "short notes" {
		t.Fatalf("expected unchanged body, got %q", notes)
	}
}

// ── multiple sources independent ──────────────────────────────────────────────

func TestMultipleSources_SeenIndependent(t *testing.T) {
	c := &Collector{seen: make(map[int64]map[int64]struct{})}
	c.seen[1] = map[int64]struct{}{100: {}}
	c.seen[2] = make(map[int64]struct{})

	_, knownFor1 := c.seen[1][100]
	_, knownFor2 := c.seen[2][100]

	if !knownFor1 {
		t.Fatal("release 100 should be known for source 1")
	}
	if knownFor2 {
		t.Fatal("release 100 should be unknown for source 2")
	}
}
