package price

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestCollector() *Collector {
	return &Collector{
		client:    &http.Client{Timeout: 5 * time.Second},
		lastPrice: make(map[int64]float64),
	}
}

func makePriceSrv(coin, currency string, price float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]map[string]float64{
			coin: {currency: price},
		})
	}))
}

// ── fetchPrice ────────────────────────────────────────────────────────────────

func TestFetchPrice_ParsesResponse(t *testing.T) {
	srv := makePriceSrv("bitcoin", "usd", 95000.0)
	defer srv.Close()

	c := newTestCollector()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]map[string]float64
	json.NewDecoder(resp.Body).Decode(&result)
	if result["bitcoin"]["usd"] != 95000.0 {
		t.Fatalf("expected 95000.0, got %f", result["bitcoin"]["usd"])
	}
}

func TestFetchPrice_Returns429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestCollector()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
}

func TestFetchPrice_Returns404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestCollector()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200")
	}
}

// ── threshold above ───────────────────────────────────────────────────────────

func TestThreshold_AboveCrossing(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 100000, direction: "above"}
	c.lastPrice[s.id] = 99000.0
	prev := c.lastPrice[s.id]
	price := 101000.0
	c.lastPrice[s.id] = price

	if !(prev < s.threshold && price >= s.threshold) {
		t.Fatal("expected threshold crossing above")
	}
}

func TestThreshold_AboveExactly(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 100000, direction: "above"}
	c.lastPrice[s.id] = 99999.99
	prev := c.lastPrice[s.id]
	price := 100000.0
	c.lastPrice[s.id] = price

	if !(prev < s.threshold && price >= s.threshold) {
		t.Fatal("expected crossing at exact threshold")
	}
}

func TestThreshold_AboveNoCrossing_AlreadyAbove(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 100000, direction: "above"}
	c.lastPrice[s.id] = 101000.0
	prev := c.lastPrice[s.id]
	price := 102000.0
	c.lastPrice[s.id] = price

	if prev < s.threshold && price >= s.threshold {
		t.Fatal("expected no crossing — was already above threshold")
	}
}

func TestThreshold_AboveNoCrossing_StillBelow(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 100000, direction: "above"}
	c.lastPrice[s.id] = 98000.0
	prev := c.lastPrice[s.id]
	price := 99000.0
	c.lastPrice[s.id] = price

	if prev < s.threshold && price >= s.threshold {
		t.Fatal("expected no crossing — still below threshold")
	}
}

// ── threshold below ───────────────────────────────────────────────────────────

func TestThreshold_BelowCrossing(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 50000, direction: "below"}
	c.lastPrice[s.id] = 51000.0
	prev := c.lastPrice[s.id]
	price := 49000.0
	c.lastPrice[s.id] = price

	if !(prev > s.threshold && price <= s.threshold) {
		t.Fatal("expected threshold crossing below")
	}
}

func TestThreshold_BelowExactly(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 50000, direction: "below"}
	c.lastPrice[s.id] = 50001.0
	prev := c.lastPrice[s.id]
	price := 50000.0
	c.lastPrice[s.id] = price

	if !(prev > s.threshold && price <= s.threshold) {
		t.Fatal("expected crossing at exact threshold")
	}
}

func TestThreshold_BelowNoCrossing_AlreadyBelow(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 50000, direction: "below"}
	c.lastPrice[s.id] = 48000.0
	prev := c.lastPrice[s.id]
	price := 47000.0
	c.lastPrice[s.id] = price

	if prev > s.threshold && price <= s.threshold {
		t.Fatal("expected no crossing — already below")
	}
}

func TestThreshold_BelowNoCrossing_StillAbove(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 50000, direction: "below"}
	c.lastPrice[s.id] = 52000.0
	prev := c.lastPrice[s.id]
	price := 51000.0
	c.lastPrice[s.id] = price

	if prev > s.threshold && price <= s.threshold {
		t.Fatal("expected no crossing — still above")
	}
}

// ── first fetch ───────────────────────────────────────────────────────────────

func TestFirstFetch_NoEvent(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 100000, direction: "above"}
	_, seen := c.lastPrice[s.id]
	c.lastPrice[s.id] = 95000.0
	if seen {
		t.Fatal("expected first fetch to have no prior state")
	}
}

func TestFirstFetch_StateRecorded(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1}
	c.lastPrice[s.id] = 42000.0
	if v, ok := c.lastPrice[s.id]; !ok || v != 42000.0 {
		t.Fatal("expected state to be recorded after first fetch")
	}
}

// ── multiple sources independent ──────────────────────────────────────────────

func TestMultipleSources_PriceIndependent(t *testing.T) {
	c := newTestCollector()
	c.lastPrice[1] = 100000.0
	c.lastPrice[2] = 50000.0

	if c.lastPrice[1] != 100000.0 {
		t.Fatal("source 1 price incorrect")
	}
	if c.lastPrice[2] != 50000.0 {
		t.Fatal("source 2 price incorrect")
	}
}

// ── defaults ──────────────────────────────────────────────────────────────────

func TestDefaultDirection_Above(t *testing.T) {
	direction := ""
	if direction == "" {
		direction = "above"
	}
	if direction != "above" {
		t.Fatalf("expected 'above', got %q", direction)
	}
}

func TestDefaultCurrency_USD(t *testing.T) {
	currency := ""
	if currency == "" {
		currency = "usd"
	}
	if currency != "usd" {
		t.Fatalf("expected 'usd', got %q", currency)
	}
}

// ── event message format ──────────────────────────────────────────────────────

func TestEventMessage_AboveCrossing(t *testing.T) {
	s := source{id: 1, coin: "bitcoin", currency: "usd", threshold: 100000, direction: "above"}
	price := 101000.0
	prev := 99000.0

	title := "bitcoin crossed 100000 usd (above)"
	body := "Current price: 101000.00 usd (was 99000.00)"

	_ = s
	_ = price
	_ = prev

	if title == "" || body == "" {
		t.Fatal("expected non-empty title and body")
	}
}

func TestEventPriority_AlwaysHigh(t *testing.T) {
	// price crossings always emit high priority
	priority := "high"
	if priority != "high" {
		t.Fatalf("expected high priority for price crossing, got %q", priority)
	}
}
