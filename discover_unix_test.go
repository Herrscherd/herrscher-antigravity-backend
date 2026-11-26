//go:build unix

package antigravity

import (
	"errors"
	"testing"
)

func TestDiscoverRunsTheModelsCommand(t *testing.T) {
	stub := writeStubScript(t, `[ "$1" = models ] || exit 3
printf '%s' "`+recordedModels+`"`)
	specs, err := discover(t.Context(), nativeAccess(stub))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("specs = %+v", specs)
	}
}

func TestDiscoverSignedOutListsNothingWithoutError(t *testing.T) {
	stub := writeStubScript(t, `echo "Error: Please sign in to view available models. Launch the CLI without arguments to sign in." >&2
exit 1`)
	specs, err := discover(t.Context(), nativeAccess(stub))
	if err != nil || specs != nil {
		t.Fatalf("specs = %+v, err = %v, want nothing and no error", specs, err)
	}
}

func TestDiscoverOverlongListingIsAnError(t *testing.T) {
	stub := writeStubScript(t, "head -c 300000 /dev/zero | tr '\\000' a")
	_, err := discover(t.Context(), nativeAccess(stub))
	if _, isTooLarge := errors.AsType[*OutputTooLargeError](err); !isTooLarge {
		t.Fatal("an overlong listing returned no error")
	}
}
