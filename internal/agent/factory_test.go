package agent

import (
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

func TestEngineSetModelAndModelName(t *testing.T) {
	eng := newEngineForTest(&fakeModel{}, tool.NewToolkit(), bypassEngine(), 10)
	if eng.ModelName() != "test-model" {
		t.Fatalf("default model: %s", eng.ModelName())
	}
	if err := eng.SetModel("gpt-4o"); err != nil {
		t.Fatalf("setmodel: %v", err)
	}
	if eng.ModelName() != "gpt-4o" {
		t.Fatalf("after set: %s", eng.ModelName())
	}
}

func TestEngineListModelsForProvider(t *testing.T) {
	eng := newEngineForTest(&fakeModel{}, tool.NewToolkit(), bypassEngine(), 10) // provider=openai
	models := eng.ListModels()
	if len(models) == 0 {
		t.Fatal("no models listed")
	}
	for _, m := range models {
		if strings.Contains(strings.ToLower(m), "claude") {
			t.Fatalf("openai list has claude model %q", m)
		}
	}
}
