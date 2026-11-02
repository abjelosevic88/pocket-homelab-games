package platforms

import (
	"strings"
	"testing"
)

func TestForExtension(t *testing.T) {
	cases := map[string]string{"nes": "nes", "sfc": "snes", "gba": "gba", "nds": "nds", "3ds": "3ds", "md": "genesis", "pce": "tg16", "rvz": "ngc,wii"}
	for ext, want := range cases {
		if got := strings.Join(ForExtension(ext), ","); got != want {
			t.Errorf("%s → %s, want %s", ext, got, want)
		}
	}
	if len(ForExtension("iso")) < 3 {
		t.Error("an .iso fits several systems")
	}
}
