//go:build !windows && !darwin

package mumblelink

import (
	"fmt"
	"os"
)

type wineHost struct {
	Bin    string
	Prefix string
	Extra  map[string]string
}

func (h wineHost) env() []string {
	env := os.Environ()
	return append(env,
		"WINEPREFIX="+h.Prefix,
		"WINELOADER="+h.Bin,
		"WINEDEBUG=-all",
	)
}

func findWineHost() (wineHost, error) {
	bin, prefix := os.Getenv("WINE"), os.Getenv("WINEPREFIX")
	if bin == "" || prefix == "" {
		return wineHost{}, fmt.Errorf("mumblelink: set WINE and WINEPREFIX to the prefix where Gw2-64.exe is running")
	}
	return wineHost{Bin: bin, Prefix: prefix, Extra: map[string]string{}}, nil
}
