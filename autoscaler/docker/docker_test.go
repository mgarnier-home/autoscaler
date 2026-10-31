package docker

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	dockerclient "github.com/docker/docker/client"
)

// runnerFromLabels est le point d'entrée de la reprise d'état : les labels du
// conteneur remplacent un fichier persisté, puisqu'ils ne peuvent pas décrire un
// conteneur qui n'existe plus.
//
// Un ID invalide doit faire échouer la reprise, jamais produire un ID nul :
// github répondrait « introuvable » au désenregistrement, donc « runner libre »,
// et l'arrêt détruirait un conteneur peut-être en plein job.
func TestRunnerFromLabels(t *testing.T) {
	tests := []struct {
		name         string
		labels       map[string]string
		wantName     string
		wantRunnerID int64
		wantOK       bool
	}{
		{
			name: "conteneur runner complet",
			labels: map[string]string{
				runnerScaleSetLabel: "local-mgarnier-1",
				runnerNameLabel:     "runner-ab12cd34",
				runnerIDLabel:       "4242",
			},
			wantName:     "runner-ab12cd34",
			wantRunnerID: 4242,
			wantOK:       true,
		},
		{
			name: "conteneur créé avant l'introduction du label d'ID",
			labels: map[string]string{
				runnerScaleSetLabel: "local-mgarnier-1",
				runnerNameLabel:     "runner-ab12cd34",
			},
			wantOK: false,
		},
		{
			name: "ID illisible",
			labels: map[string]string{
				runnerNameLabel: "runner-ab12cd34",
				runnerIDLabel:   "pas-un-nombre",
			},
			wantOK: false,
		},
		{
			name: "ID nul : github le prendrait pour un runner introuvable",
			labels: map[string]string{
				runnerNameLabel: "runner-ab12cd34",
				runnerIDLabel:   "0",
			},
			wantOK: false,
		},
		{
			name: "ID négatif",
			labels: map[string]string{
				runnerNameLabel: "runner-ab12cd34",
				runnerIDLabel:   "-1",
			},
			wantOK: false,
		},
		{
			name: "nom manquant : le runner serait introuvable dans runnerState",
			labels: map[string]string{
				runnerIDLabel: "4242",
			},
			wantOK: false,
		},
		{
			name:   "conteneur sans aucun label",
			labels: map[string]string{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, runnerID, ok := runnerFromLabels(tt.labels)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, attendu %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				// Une reprise refusée ne doit rien laisser fuiter : un ID nul
				// renvoyé par erreur serait interprété comme « runner libre ».
				if name != "" || runnerID != 0 {
					t.Fatalf("reprise refusée mais valeurs non nulles: name=%q runnerID=%d", name, runnerID)
				}
				return
			}
			if name != tt.wantName || runnerID != tt.wantRunnerID {
				t.Fatalf("name=%q runnerID=%d, attendu %q et %d", name, runnerID, tt.wantName, tt.wantRunnerID)
			}
		})
	}
}

// ShortID doit rendre exactement le préfixe que docker affiche, et ne pas
// tronquer une chaîne déjà courte.
func TestShortID(t *testing.T) {
	long := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if got := ShortID(long); got != "0123456789ab" {
		t.Fatalf("ShortID(long) = %q, attendu %q", got, "0123456789ab")
	}
	if got := ShortID("abc"); got != "abc" {
		t.Fatalf("ShortID(court) = %q, attendu %q", got, "abc")
	}
}

// fakeDockerHost simule l'API docker d'un hôte qui ne connaît que les réseaux
// donnés.
func fakeDockerHost(t *testing.T, networks ...string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.URL.Path, "/networks/") || !slices.Contains(networks, name) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"network ` + name + ` not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"Name":"` + name + `","Id":"abc"}`))
	}))
	t.Cleanup(srv.Close)

	client, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost("tcp://"+srv.Listener.Addr().String()),
		dockerclient.WithVersion("1.47"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{Client: client}
}

func TestCheckNetwork(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	pool := &Pool{logger: logger, clients: []*Client{fakeDockerHost(t, "autoscaler-buildkit"), fakeDockerHost(t, "autoscaler-buildkit")}}
	if err := pool.CheckNetwork(ctx, "autoscaler-buildkit"); err != nil {
		t.Fatalf("réseau présent sur tous les hôtes : %v", err)
	}

	pool = &Pool{logger: logger, clients: []*Client{fakeDockerHost(t, "autoscaler-buildkit"), fakeDockerHost(t)}}
	err := pool.CheckNetwork(ctx, "autoscaler-buildkit")
	if err == nil || !strings.Contains(err.Error(), "autoscaler-buildkit") {
		t.Fatalf("réseau absent d'un hôte : erreur %v, attendu une erreur qui nomme le réseau", err)
	}

	// Sans réseau configuré, les runners restent sur le réseau par défaut : rien
	// à vérifier, aucun appel au daemon.
	pool = &Pool{logger: logger, clients: []*Client{fakeDockerHost(t)}}
	if err := pool.CheckNetwork(ctx, ""); err != nil {
		t.Fatalf("aucun réseau configuré : %v", err)
	}
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		want    map[string]string
		wantErr bool
	}{
		{name: "aucun label", entries: nil, want: map[string]string{}},
		{name: "entrées vides ignorées", entries: []string{"", "  "}, want: map[string]string{}},
		{
			name:    "plusieurs labels, espaces retirés",
			entries: []string{"com.example.team=ci", " traefik.enable = false "},
			want:    map[string]string{"com.example.team": "ci", "traefik.enable": "false"},
		},
		{name: "valeur vide acceptée", entries: []string{"flag="}, want: map[string]string{"flag": ""}},
		{name: "valeur contenant un =", entries: []string{"a=b=c"}, want: map[string]string{"a": "b=c"}},
		{name: "sans =", entries: []string{"nope"}, wantErr: true},
		{name: "clé vide", entries: []string{"=value"}, wantErr: true},
		{name: "label interne refusé", entries: []string{runnerIDLabel + "=1"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLabels(tt.entries)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLabels(%q) = %v, want error", tt.entries, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLabels(%q) error: %v", tt.entries, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("ParseLabels(%q) = %v, want %v", tt.entries, got, tt.want)
			}
		})
	}
}
