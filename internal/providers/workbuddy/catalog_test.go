package workbuddy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogCreditsMeteredParsing(t *testing.T) {
	for _, tc := range []struct {
		credits string
		metered bool
	}{
		{"x0.79", true},
		{"x0.34 credits", true},
		{" 0.50 ", true},
		{"X.5 CREDITS", true},
		{"x0.00", false},
		{"", false},
		{"credits", false},
		{"unknown", false},
		{"model 5 pricing pending", false},
		{"x-0.79 credits", false},
		{"NaN", false},
		{"+Inf", false},
		{"1e999", false},
	} {
		t.Run(tc.credits, func(t *testing.T) {
			if got := catalogCreditsMetered(tc.credits); got != tc.metered {
				t.Fatalf("credits=%q metered=%v want %v", tc.credits, got, tc.metered)
			}
		})
	}
}

func TestCatalogModelDeclaresOnlyKnownMeteredModels(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entry   catalogModelEntry
		free    bool
		metered bool
	}{
		{"paid", catalogModelEntry{ID: "glm-5.3", Credits: "x0.79"}, false, true},
		{"free", catalogModelEntry{ID: "hy3", Credits: "x0.00 credits"}, true, false},
		{"blank", catalogModelEntry{ID: "glm-unknown"}, false, false},
		{"unparseable", catalogModelEntry{ID: "glm-unknown", Credits: "not priced"}, false, false},
		{"auto-id", catalogModelEntry{ID: " AUTO ", Credits: "x0.79"}, false, false},
		{"auto-default-id", catalogModelEntry{ID: "default-model", Credits: "x0.79"}, false, false},
		{"auto-name", catalogModelEntry{ID: "model-selector", Name: "Auto", Credits: "x0.79"}, false, false},
		{"missing-id", catalogModelEntry{Credits: "x0.79"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := catalogModel(tc.entry)
			if info.Free != tc.free || info.Metered != tc.metered {
				t.Fatalf("model=%+v want free=%v metered=%v", info, tc.free, tc.metered)
			}
			encoded, err := json.Marshal(info)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "metered") || strings.Contains(string(encoded), "Metered") {
				t.Fatalf("internal metering declaration leaked into public model JSON: %s", encoded)
			}
		})
	}
}
