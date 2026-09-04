package orbitproxy_test

import (
	"os"
	"strings"
	"testing"

	orbitproxy "github.com/orbitproxy/orbitproxy-go"
)

func TestReleaseVersionMatchesVERSIONFile(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if want == "" {
		t.Fatal("VERSION file is empty")
	}
	if want[0] != 'v' && want[0] != 'V' {
		want = "v" + want
	}
	if orbitproxy.ReleaseVersion != want {
		t.Fatalf("ReleaseVersion = %q, VERSION file normalizes to %q", orbitproxy.ReleaseVersion, want)
	}
}

func TestVersionFromBuildInfo(t *testing.T) {
	t.Parallel()

	v := orbitproxy.Version()
	switch v {
	case "", "dev", "devel", "(devel)":
		t.Fatalf("Version() = %q, want a release semver", v)
	}
}

func TestRegisterRejectsNonSemver(t *testing.T) {
	t.Parallel()

	_, err := orbitproxy.Register(t.Context(), orbitproxy.RegisterOptions{
		AuthToken:  "tok_test",
		MachineKey: "ck_test",
		APIURL:     "http://127.0.0.1:1",
		Version:    "dev",
	})
	if err == nil {
		t.Fatal("Register unexpectedly succeeded")
	}
}
