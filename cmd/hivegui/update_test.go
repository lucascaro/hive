package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.4", -1},
		{"1.2.4", "1.2.3", 1},
		{"1.2.3", "1.2.3", 0},
		{"1.10.0", "1.9.9", 1},
		{"1.9.9", "1.10.0", -1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0-rc2", -1},
		{"1.0", "1.0.0", 0},
		{"foo", "bar", 0},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// stubReleases swaps updateReleasesAPI / updateURLPrefix for the
// duration of one test and restores them on cleanup. The handler is
// the test's stub for the GitHub releases endpoint.
func stubReleases(t *testing.T, handler http.HandlerFunc, urlPrefix string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prevAPI, prevPrefix := updateReleasesAPI, updateURLPrefix
	updateReleasesAPI = srv.URL + "/releases/latest"
	if urlPrefix == "" {
		updateURLPrefix = srv.URL + "/"
	} else {
		updateURLPrefix = urlPrefix
	}
	t.Cleanup(func() {
		updateReleasesAPI = prevAPI
		updateURLPrefix = prevPrefix
	})
}

func TestCheckForUpdate_DevBuildSkipped(t *testing.T) {
	defer buildinfo.SetVersionForTest("dev")()
	stubReleases(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("dev build must not call the releases API")
	}, "")
	a := &App{}
	info, err := a.CheckForUpdate()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !info.Skipped || info.Available {
		t.Fatalf("dev build: want Skipped && !Available, got %+v", info)
	}
}

func TestCheckForUpdate_NewerAvailable(t *testing.T) {
	defer buildinfo.SetVersionForTest("0.4.1")()
	stubReleases(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.5.0","html_url":"` + updateURLPrefix + `releases/tag/v0.5.0"}`))
	}, "")
	a := &App{}
	info, err := a.CheckForUpdate()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !info.Available || info.Latest != "0.5.0" || info.URL == "" {
		t.Fatalf("want Available=true, Latest=0.5.0, URL set; got %+v", info)
	}
}

func TestCheckForUpdate_SameVersionNotAvailable(t *testing.T) {
	defer buildinfo.SetVersionForTest("0.5.0")()
	stubReleases(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.5.0","html_url":"` + updateURLPrefix + `releases/tag/v0.5.0"}`))
	}, "")
	a := &App{}
	info, err := a.CheckForUpdate()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if info.Available {
		t.Fatalf("same version: want !Available, got %+v", info)
	}
}

func TestCheckForUpdate_BadURLPrefixStillReportsAvailable(t *testing.T) {
	// Regression: a tampered html_url must NOT silently rewrite
	// Available=true into Available=false. The URL is dropped (so the
	// frontend hides the Download button), but the user is still told
	// an update exists.
	defer buildinfo.SetVersionForTest("0.4.1")()
	stubReleases(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.5.0","html_url":"file:///etc/passwd"}`))
	}, "https://github.com/lucascaro/hive/")
	a := &App{}
	info, err := a.CheckForUpdate()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !info.Available {
		t.Fatalf("want Available=true even with bad URL; got %+v", info)
	}
	if info.URL != "" {
		t.Fatalf("want URL='' when prefix fails; got %q", info.URL)
	}
}

func TestCheckForUpdate_Non200Errors(t *testing.T) {
	defer buildinfo.SetVersionForTest("0.4.1")()
	stubReleases(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, "")
	a := &App{}
	_, err := a.CheckForUpdate()
	if err == nil {
		t.Fatal("want error on non-200")
	}
}

func TestCheckForUpdate_MalformedJSON(t *testing.T) {
	defer buildinfo.SetVersionForTest("0.4.1")()
	stubReleases(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}, "")
	a := &App{}
	_, err := a.CheckForUpdate()
	if err == nil {
		t.Fatal("want error on malformed JSON")
	}
}
