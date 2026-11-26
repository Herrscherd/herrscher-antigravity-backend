package antigravity

import (
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func registered(t *testing.T) contracts.Plugin {
	t.Helper()
	plugins := contracts.Default.Backends()
	for index := range plugins {
		if plugins[index].Manifest.Kind == "antigravity" {
			return plugins[index]
		}
	}
	t.Fatal("antigravity is not registered")
	return contracts.Plugin{}
}

func TestStaticCatalogIsValid(t *testing.T) {
	if err := contracts.ValidateModels("antigravity", registered(t).Manifest.Models); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryRefusesAnUnknownEffort(t *testing.T) {
	_, err := registered(t).Backend(t.Context(), contracts.PluginConfig{Settings: map[string]string{"effort": "extreme"}})
	if !errors.Is(err, ErrInvalidSetting) {
		t.Fatalf("err = %v, want ErrInvalidSetting", err)
	}
}

func TestFactoryAcceptsEveryEffortOfAgy(t *testing.T) {
	for _, effort := range []string{"", "low", "medium", "High", "xhigh", "max"} {
		if _, err := registered(t).Backend(t.Context(), contracts.PluginConfig{Settings: map[string]string{"effort": effort}}); err != nil {
			t.Fatalf("effort %q: %v", effort, err)
		}
	}
}

func TestFactoryResumesThePersistedConversation(t *testing.T) {
	backend, err := registered(t).Backend(t.Context(), contracts.PluginConfig{Settings: map[string]string{"resume": "conv-3"}})
	if err != nil {
		t.Fatal(err)
	}
	aware, ok := backend.(contracts.ResumeAware)
	if !ok || aware.ResumeToken() != "conv-3" {
		t.Fatal("factory dropped the persisted conversation")
	}
}
