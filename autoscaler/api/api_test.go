package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgarnier-home/utils/buildinfo"
	"mgarnier11.fr/docker-autoscaler/scaler"
)

func TestGetStats(t *testing.T) {
	handler := NewHandler(func() scaler.Stats {
		return scaler.Stats{ScaleSetName: "apollon", Runners: scaler.RunnerStats{Idle: 2, Busy: 1, Min: 2, Max: 10}}
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d, attendu 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type %q, attendu application/json", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("JSON invalide : %v", err)
	}
	runners := got["runners"].(map[string]any)
	if got["scaleSetName"] != "apollon" || runners["busy"] != 1.0 || got["github"] != nil {
		t.Fatalf("corps inattendu : %s", rec.Body.String())
	}
}

func TestAutresRoutesEtMethodes(t *testing.T) {
	handler := NewHandler(func() scaler.Stats { return scaler.Stats{} })

	for _, tt := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/stats", http.StatusMethodNotAllowed},
		{http.MethodGet, "/", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
		if rec.Code != tt.want {
			t.Errorf("%s %s : statut %d, attendu %d", tt.method, tt.path, rec.Code, tt.want)
		}
	}
}

func TestGetVersion(t *testing.T) {
	handler := NewHandler(func() scaler.Stats { return scaler.Stats{} })

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d, attendu 200", rec.Code)
	}
	var got buildinfo.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("JSON invalide %q : %v", rec.Body.String(), err)
	}
	if got != buildinfo.Get() {
		t.Fatalf("corps %+v, attendu %+v", got, buildinfo.Get())
	}
}
