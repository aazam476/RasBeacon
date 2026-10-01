package clock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAgreedTime(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	floor := base.Add(-24 * time.Hour)
	tests := []struct {
		name    string
		samples []time.Time
		want    time.Time
		ok      bool
	}{
		{"two agree", []time.Time{base, base.Add(2 * time.Second)}, base.Add(2 * time.Second), true},
		{"one wrong, two agree", []time.Time{base.Add(-90 * 24 * time.Hour), base, base.Add(time.Second)}, base.Add(time.Second), true},
		{"only one answer", []time.Time{base}, time.Time{}, false},
		{"none agree", []time.Time{base, base.Add(time.Minute), base.Add(2 * time.Minute)}, time.Time{}, false},
		{"before the floor", []time.Time{floor.Add(-time.Hour), floor.Add(-time.Hour)}, time.Time{}, false},
		{"failed requests", []time.Time{{}, {}, base}, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AgreedTime(tt.samples, floor)
			if ok != tt.ok || !got.Equal(tt.want) {
				t.Errorf("got %v %v, want %v %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestFetchDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", "Thu, 01 Oct 2026 12:00:00 GMT")
	}))
	defer server.Close()
	got, err := FetchDate(context.Background(), server.Client(), server.URL)
	if err != nil || !got.Equal(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("got %v, %v", got, err)
	}
}
