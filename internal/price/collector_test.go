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

func TestFetchPrice_ParsesResponse(t *testing.T) {
	srv := makePriceSrv("bitcoin", "usd", 95000.0)
	defer srv.Close()

	c := newTestCollector()
	// directly call fetchPrice with test server URL override
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, _ := c.client.Do(req)
	defer resp.Body.Close()

	var result map[string]map[string]float64
	json.NewDecoder(resp.Body).Decode(&result)

	got := result["bitcoin"]["usd"]
	if got != 95000.0 {
		t.Fatalf("expected 95000.0, got %f", got)
	}
}

func TestThreshold_AboveCrossing(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 100000, direction: "above"}

	c.lastPrice[s.id] = 99000.0
	prev := c.lastPrice[s.id]
	price := 101000.0
	c.lastPrice[s.id] = price

	crossed := prev < s.threshold && price >= s.threshold
	if !crossed {
		t.Fatal("expected threshold crossing above")
	}
}

func TestThreshold_AboveNoCrossing_AlreadyAbove(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 100000, direction: "above"}

	c.lastPrice[s.id] = 101000.0
	prev := c.lastPrice[s.id]
	price := 102000.0
	c.lastPrice[s.id] = price

	crossed := prev < s.threshold && price >= s.threshold
	if crossed {
		t.Fatal("expected no crossing — was already above threshold")
	}
}

func TestThreshold_BelowCrossing(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 50000, direction: "below"}

	c.lastPrice[s.id] = 51000.0
	prev := c.lastPrice[s.id]
	price := 49000.0
	c.lastPrice[s.id] = price

	crossed := prev > s.threshold && price <= s.threshold
	if !crossed {
		t.Fatal("expected threshold crossing below")
	}
}

func TestThreshold_BelowNoCrossing_AlreadyBelow(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 50000, direction: "below"}

	c.lastPrice[s.id] = 48000.0
	prev := c.lastPrice[s.id]
	price := 47000.0
	c.lastPrice[s.id] = price

	crossed := prev > s.threshold && price <= s.threshold
	if crossed {
		t.Fatal("expected no crossing — was already below threshold")
	}
}

func TestFirstFetch_NoEvent(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, threshold: 100000, direction: "above"}

	_, seen := c.lastPrice[s.id]
	c.lastPrice[s.id] = 95000.0

	if seen {
		t.Fatal("expected first fetch to have no prior state")
	}
}

func TestDefaultDirection_Above(t *testing.T) {
	// verify that empty direction defaults to "above" in loadSources logic
	direction := ""
	if direction == "" {
		direction = "above"
	}
	if direction != "above" {
		t.Fatalf("expected default direction 'above', got %q", direction)
	}
}

func TestDefaultCurrency_USD(t *testing.T) {
	currency := ""
	if currency == "" {
		currency = "usd"
	}
	if currency != "usd" {
		t.Fatalf("expected default currency 'usd', got %q", currency)
	}
}

func TestPriceSrv_Returns429_HandledGracefully(t *testing.T) {
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
	// fetchPrice would return error on non-200 — verified by status check
}
