package games

import (
	"errors"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// covers registry lookup for the supported games and an unknown one
func TestLookup(t *testing.T) {
	t.Parallel()

	for _, name := range []string{instance.GameCoreKeeper, instance.GameProjectZomboid} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reg, err := Lookup(name)
			if err != nil {
				t.Fatalf("Lookup(%q) error = %v", name, err)
			}
			if reg.Game != name || reg.Installer == nil || reg.Runner == nil {
				t.Errorf("Lookup(%q) = %+v, want a complete registration", name, reg)
			}
		})
	}

	if _, err := Lookup("nope"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Lookup(nope) error = %v, want ErrUnsupported", err)
	}
}
