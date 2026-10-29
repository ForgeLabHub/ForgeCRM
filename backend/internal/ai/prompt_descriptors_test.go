package ai

import "testing"

// The prompt's tool contract must come from the registry, so the tools the model
// is told about are the tools the server will authorise. This pins that link.
func TestPromptDescriptorsComeFromTheRegistry(t *testing.T) {
	r := NewToolRegistry(nil, nil)
	got := r.PromptDescriptors()

	if len(got) == 0 {
		t.Fatal("the registry produced no prompt descriptors")
	}

	byName := map[string]ToolDescriptor{}
	for _, d := range got {
		byName[d.Name] = d
	}

	// A known read tool must be present and must not claim to need confirmation.
	search, ok := byName["search_accounts"]
	if !ok {
		t.Fatal("search_accounts is missing from the prompt descriptors")
	}
	if search.RequiresConfirmation {
		t.Error("search_accounts is a read but is advertised as requiring confirmation")
	}

	// A known mutating tool must be present and must be advertised as confirm-required,
	// matching the registry's Dangerous flag.
	create, ok := byName["create_task"]
	if !ok {
		t.Fatal("create_task is missing from the prompt descriptors")
	}
	if !create.RequiresConfirmation {
		t.Error("create_task mutates state but is not advertised as requiring confirmation")
	}

	// Every descriptor must carry a description, or the model has a name with no idea
	// what it does.
	for _, d := range got {
		if d.Description == "" {
			t.Errorf("tool %q has no description in the prompt contract", d.Name)
		}
	}
}

// A nil registry must not panic: the prompt is assembled before AI may be configured.
func TestPromptDescriptorsTolerateANilRegistry(t *testing.T) {
	var r *ToolRegistry
	if got := r.PromptDescriptors(); len(got) != 0 {
		t.Errorf("a nil registry returned %d descriptors; expected none", len(got))
	}
}
