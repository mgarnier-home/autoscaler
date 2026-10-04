package docker

import "testing"

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
