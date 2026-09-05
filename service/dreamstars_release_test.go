/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchDreamstarsReleaseAcceptsTrustedManifest(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/releases":
			writer.Header().Set("Content-Type", "application/json")
			_, err := writer.Write([]byte(`[{"tag_name":"dreamstars-20260905-120000-123456789abc","published_at":"2026-09-05T12:00:00Z","assets":[{"name":"dreamstars-release.json","browser_download_url":"` + server.URL + `/manifest"}]}]`))
			if err != nil {
				t.Errorf("write releases response: %v", err)
			}
		case "/manifest":
			writer.Header().Set("Content-Type", "application/json")
			_, err := writer.Write([]byte(trustedDreamstarsManifestJSON("MOON-DREAM-STARS/new-api")))
			if err != nil {
				t.Errorf("write manifest response: %v", err)
			}
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	release, err := fetchDreamstarsRelease(
		context.Background(),
		server.Client(),
		server.URL+"/releases",
		func(rawURL, _ string) bool { return rawURL == server.URL+"/manifest" },
	)

	require.NoError(t, err)
	assert.Equal(t, "dreamstars-20260905-120000-123456789abc", release.Version)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", release.Commit)
	assert.Equal(t, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", release.Digest)
	assert.Equal(t, []string{"linux/amd64"}, release.Platforms)
}

func TestFetchDreamstarsReleaseRejectsManifestFromAnotherRepository(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/releases":
			writer.Header().Set("Content-Type", "application/json")
			_, err := writer.Write([]byte(`[{"tag_name":"dreamstars-20260905-120000-123456789abc","published_at":"2026-09-05T12:00:00Z","assets":[{"name":"dreamstars-release.json","browser_download_url":"` + server.URL + `/manifest"}]}]`))
			if err != nil {
				t.Errorf("write releases response: %v", err)
			}
		case "/manifest":
			writer.Header().Set("Content-Type", "application/json")
			_, err := writer.Write([]byte(trustedDreamstarsManifestJSON("someone-else/new-api")))
			if err != nil {
				t.Errorf("write manifest response: %v", err)
			}
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	_, err := fetchDreamstarsRelease(
		context.Background(),
		server.Client(),
		server.URL+"/releases",
		func(rawURL, _ string) bool { return rawURL == server.URL+"/manifest" },
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "source is not trusted")
}

func TestFetchDreamstarsReleaseRejectsNonDreamstarsTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`[{"tag_name":"v0.0.0","published_at":"2026-09-05T12:00:00Z"}]`))
		if err != nil {
			t.Errorf("write releases response: %v", err)
		}
	}))
	defer server.Close()

	_, err := fetchDreamstarsRelease(
		context.Background(),
		server.Client(),
		server.URL,
		func(string, string) bool { return false },
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no approved Dreamstars release found")
}

func TestDreamstarsReleaseCacheUsesFreshValueAndMarksStaleFallback(t *testing.T) {
	withDreamstarsReleaseTestState(t, func() {
		now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
		dreamstarsReleaseNow = func() time.Time { return now }
		calls := 0
		dreamstarsReleaseFetch = func(context.Context) (*DreamstarsRelease, error) {
			calls++
			if calls == 1 {
				return trustedDreamstarsRelease(), nil
			}
			return nil, errors.New("GitHub unavailable")
		}

		first := GetDreamstarsReleaseUpdate(context.Background(), "dreamstars-20260905-120000-123456789abc")
		second := GetDreamstarsReleaseUpdate(context.Background(), "dreamstars-20260905-120000-123456789abc")
		now = now.Add(dreamstarsReleaseCacheTTL + time.Second)
		third := GetDreamstarsReleaseUpdate(context.Background(), "dreamstars-20260905-120000-123456789abc")

		assert.Equal(t, DreamstarsReleaseSourceLive, first.Source)
		assert.Equal(t, DreamstarsReleaseSourceCached, second.Source)
		assert.Equal(t, DreamstarsReleaseSourceStale, third.Source)
		assert.Equal(t, DreamstarsReleaseStatusUnavailable, third.Status)
		assert.Equal(t, 2, calls)
	})
}

func TestDreamstarsReleaseCacheCoalescesConcurrentFetches(t *testing.T) {
	withDreamstarsReleaseTestState(t, func() {
		started := make(chan struct{})
		releaseFetch := make(chan struct{})
		var calls atomic.Int32
		dreamstarsReleaseFetch = func(context.Context) (*DreamstarsRelease, error) {
			calls.Add(1)
			close(started)
			<-releaseFetch
			return trustedDreamstarsRelease(), nil
		}

		results := make(chan DreamstarsReleaseUpdate, 2)
		go func() {
			results <- GetDreamstarsReleaseUpdate(context.Background(), "v0.0.0")
		}()
		<-started
		go func() {
			results <- GetDreamstarsReleaseUpdate(context.Background(), "v0.0.0")
		}()
		close(releaseFetch)

		first := <-results
		second := <-results
		assert.Equal(t, int32(1), calls.Load())
		assert.Equal(t, DreamstarsReleaseStatusCurrentUnrecognized, first.Status)
		assert.Equal(t, DreamstarsReleaseStatusCurrentUnrecognized, second.Status)
		assert.Contains(t, []string{DreamstarsReleaseSourceLive, DreamstarsReleaseSourceCached}, first.Source)
		assert.Contains(t, []string{DreamstarsReleaseSourceLive, DreamstarsReleaseSourceCached}, second.Source)
	})
}

func TestDreamstarsReleaseUpdateMarksUnavailableAndUnrecognizedVersions(t *testing.T) {
	withDreamstarsReleaseTestState(t, func() {
		dreamstarsReleaseFetch = func(context.Context) (*DreamstarsRelease, error) {
			return nil, errors.New("GitHub unavailable")
		}
		unavailable := GetDreamstarsReleaseUpdate(context.Background(), "v0.0.0")
		assert.Equal(t, DreamstarsReleaseStatusUnavailable, unavailable.Status)
		assert.Equal(t, DreamstarsReleaseSourceUnavailable, unavailable.Source)
		assert.False(t, unavailable.CheckedAt.IsZero())

		dreamstarsReleaseCacheStore = &dreamstarsReleaseCache{}
		dreamstarsReleaseFetch = func(context.Context) (*DreamstarsRelease, error) {
			return trustedDreamstarsRelease(), nil
		}
		unrecognized := GetDreamstarsReleaseUpdate(context.Background(), "v0.0.0")
		assert.Equal(t, DreamstarsReleaseStatusCurrentUnrecognized, unrecognized.Status)
	})
}

func TestCompareDreamstarsReleaseVersions(t *testing.T) {
	latest := "dreamstars-20260905-120000-123456789abc"
	cases := []struct {
		name       string
		current    string
		comparison int
		recognized bool
	}{
		{
			name:       "older approved version",
			current:    "dreamstars-20260904-120000-123456789abc",
			comparison: -1,
			recognized: true,
		},
		{
			name:       "same version",
			current:    latest,
			comparison: 0,
			recognized: true,
		},
		{
			name:       "newer unknown version",
			current:    "dreamstars-20260906-120000-123456789abc",
			comparison: 1,
			recognized: true,
		},
		{
			name:       "same timestamp different commit",
			current:    "dreamstars-20260905-120000-abcdef012345",
			comparison: 0,
			recognized: false,
		},
		{
			name:       "malformed version",
			current:    "v0.0.0",
			comparison: 0,
			recognized: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			comparison, recognized := compareDreamstarsReleaseVersions(testCase.current, latest)
			assert.Equal(t, testCase.comparison, comparison)
			assert.Equal(t, testCase.recognized, recognized)
		})
	}
}

func TestDreamstarsReleaseUpdateMarksOlderReleaseAsAvailable(t *testing.T) {
	withDreamstarsReleaseTestState(t, func() {
		dreamstarsReleaseFetch = func(context.Context) (*DreamstarsRelease, error) {
			return trustedDreamstarsRelease(), nil
		}

		update := GetDreamstarsReleaseUpdate(
			context.Background(),
			"dreamstars-20260904-120000-123456789abc",
		)

		assert.Equal(t, DreamstarsReleaseStatusUpdateAvailable, update.Status)
	})
}

func trustedDreamstarsManifestJSON(repository string) string {
	return `{
  "schema_version": 1,
  "release_tag": "dreamstars-20260905-120000-123456789abc",
  "version": "dreamstars-20260905-120000-123456789abc",
  "repository": "` + repository + `",
  "branch": "feature/dreamstars-homepage",
  "commit": "0123456789abcdef0123456789abcdef01234567",
  "image": "ghcr.io/moon-dream-stars/new-api",
  "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "platforms": ["linux/amd64"],
  "published_at": "2026-09-05T12:00:00Z",
  "signature": {
    "type": "cosign-keyless",
    "issuer": "https://token.actions.githubusercontent.com",
    "identity": "https://github.com/MOON-DREAM-STARS/new-api/.github/workflows/dreamstars-fork-release.yml@refs/heads/feature/dreamstars-homepage"
  }
}`
}

func trustedDreamstarsRelease() *DreamstarsRelease {
	return &DreamstarsRelease{
		ReleaseTag:  "dreamstars-20260905-120000-123456789abc",
		Version:     "dreamstars-20260905-120000-123456789abc",
		Commit:      "0123456789abcdef0123456789abcdef01234567",
		PublishedAt: time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC),
		ReleaseURL:  "https://github.com/MOON-DREAM-STARS/new-api/releases/tag/dreamstars-20260905-120000-123456789abc",
		Image:       "ghcr.io/moon-dream-stars/new-api",
		Digest:      "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Platforms:   []string{"linux/amd64"},
	}
}

func withDreamstarsReleaseTestState(t *testing.T, test func()) {
	t.Helper()
	originalFetch := dreamstarsReleaseFetch
	originalNow := dreamstarsReleaseNow
	originalTTL := dreamstarsReleaseCacheTTL
	originalCache := dreamstarsReleaseCacheStore
	dreamstarsReleaseCacheStore = &dreamstarsReleaseCache{}
	t.Cleanup(func() {
		dreamstarsReleaseFetch = originalFetch
		dreamstarsReleaseNow = originalNow
		dreamstarsReleaseCacheTTL = originalTTL
		dreamstarsReleaseCacheStore = originalCache
	})
	test()
}
