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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	DreamstarsReleaseStatusUpToDate                  = "up_to_date"
	DreamstarsReleaseStatusUpdateAvailable           = "update_available"
	DreamstarsReleaseStatusCurrentUnrecognized       = "current_release_unrecognized"
	DreamstarsReleaseStatusUnavailable               = "unavailable"
	DreamstarsReleaseSourceLive                      = "live"
	DreamstarsReleaseSourceCached                    = "cached"
	DreamstarsReleaseSourceStale                     = "stale"
	DreamstarsReleaseSourceUnavailable               = "unavailable"
	dreamstarsReleaseRepository                      = "MOON-DREAM-STARS/new-api"
	dreamstarsReleaseBranch                          = "feature/dreamstars-homepage"
	dreamstarsReleaseAssetName                       = "dreamstars-release.json"
	dreamstarsReleaseImage                           = "ghcr.io/moon-dream-stars/new-api"
	dreamstarsReleaseSignatureType                   = "cosign-keyless"
	dreamstarsReleaseSignatureIssuer                 = "https://token.actions.githubusercontent.com"
	dreamstarsReleaseSignatureIdentity               = "https://github.com/MOON-DREAM-STARS/new-api/.github/workflows/dreamstars-fork-release.yml@refs/heads/feature/dreamstars-homepage"
	dreamstarsReleaseGitHubReleasesURL               = "https://api.github.com/repos/MOON-DREAM-STARS/new-api/releases?per_page=20"
	dreamstarsReleaseCacheDefaultTTL                 = 15 * time.Minute
	dreamstarsReleaseResponseMaxBytes          int64 = 1024 * 1024
)

var (
	dreamstarsReleaseDigestPattern  = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	dreamstarsReleaseCommitPattern  = regexp.MustCompile(`^[a-f0-9]{40}$`)
	dreamstarsReleaseVersionPattern = regexp.MustCompile(`^dreamstars-([0-9]{8})-([0-9]{6})-([a-f0-9]{7,40})$`)
	dreamstarsReleaseHTTPClient     = &http.Client{Timeout: 8 * time.Second}
	dreamstarsReleaseFetch          = fetchLatestDreamstarsRelease
	dreamstarsReleaseNow            = time.Now
	dreamstarsReleaseCacheTTL       = dreamstarsReleaseCacheDefaultTTL
	dreamstarsReleaseCacheStore     = &dreamstarsReleaseCache{}
)

type DreamstarsReleaseUpdate struct {
	CurrentVersion string             `json:"current_version"`
	Latest         *DreamstarsRelease `json:"latest,omitempty"`
	Status         string             `json:"status"`
	Source         string             `json:"source"`
	CheckedAt      time.Time          `json:"checked_at"`
}

type DreamstarsRelease struct {
	ReleaseTag  string    `json:"release_tag"`
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	PublishedAt time.Time `json:"published_at"`
	ReleaseURL  string    `json:"release_url"`
	Image       string    `json:"image"`
	Digest      string    `json:"digest"`
	Platforms   []string  `json:"platforms"`
}

type dreamstarsGitHubRelease struct {
	TagName     string                         `json:"tag_name"`
	Draft       bool                           `json:"draft"`
	Prerelease  bool                           `json:"prerelease"`
	PublishedAt time.Time                      `json:"published_at"`
	Assets      []dreamstarsGitHubReleaseAsset `json:"assets"`
}

type dreamstarsGitHubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type dreamstarsReleaseManifest struct {
	SchemaVersion int                        `json:"schema_version"`
	ReleaseTag    string                     `json:"release_tag"`
	Version       string                     `json:"version"`
	Repository    string                     `json:"repository"`
	Branch        string                     `json:"branch"`
	Commit        string                     `json:"commit"`
	Image         string                     `json:"image"`
	Digest        string                     `json:"digest"`
	Platforms     []string                   `json:"platforms"`
	PublishedAt   time.Time                  `json:"published_at"`
	Signature     dreamstarsReleaseSignature `json:"signature"`
}

type dreamstarsReleaseSignature struct {
	Type     string `json:"type"`
	Issuer   string `json:"issuer"`
	Identity string `json:"identity"`
}

type dreamstarsReleaseCache struct {
	mu        sync.Mutex
	latest    *DreamstarsRelease
	checkedAt time.Time
	inFlight  chan struct{}
}

func GetDreamstarsReleaseUpdate(ctx context.Context, currentVersion string) DreamstarsReleaseUpdate {
	latest, source, checkedAt := dreamstarsReleaseCacheStore.get(ctx)
	result := DreamstarsReleaseUpdate{
		CurrentVersion: currentVersion,
		Latest:         latest,
		Source:         source,
		CheckedAt:      checkedAt,
	}
	if latest == nil || source == DreamstarsReleaseSourceStale {
		result.Status = DreamstarsReleaseStatusUnavailable
		return result
	}
	comparison, recognized := compareDreamstarsReleaseVersions(currentVersion, latest.Version)
	if !recognized {
		result.Status = DreamstarsReleaseStatusCurrentUnrecognized
		return result
	}
	if comparison == 0 {
		result.Status = DreamstarsReleaseStatusUpToDate
	} else if comparison < 0 {
		result.Status = DreamstarsReleaseStatusUpdateAvailable
	} else {
		result.Status = DreamstarsReleaseStatusCurrentUnrecognized
	}
	return result
}

func compareDreamstarsReleaseVersions(currentVersion, latestVersion string) (int, bool) {
	currentTime, currentOK := parseDreamstarsReleaseVersion(currentVersion)
	latestTime, latestOK := parseDreamstarsReleaseVersion(latestVersion)
	if !currentOK || !latestOK {
		return 0, false
	}
	if currentVersion == latestVersion {
		return 0, true
	}
	if currentTime.Before(latestTime) {
		return -1, true
	}
	if currentTime.After(latestTime) {
		return 1, true
	}
	return 0, false
}

func parseDreamstarsReleaseVersion(version string) (time.Time, bool) {
	matches := dreamstarsReleaseVersionPattern.FindStringSubmatch(version)
	if matches == nil {
		return time.Time{}, false
	}
	parsed, err := time.Parse("20060102-150405", matches[1]+"-"+matches[2])
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func (cache *dreamstarsReleaseCache) get(ctx context.Context) (*DreamstarsRelease, string, time.Time) {
	for {
		now := dreamstarsReleaseNow()
		cache.mu.Lock()
		if cache.latest != nil && now.Sub(cache.checkedAt) < dreamstarsReleaseCacheTTL {
			latest := cloneDreamstarsRelease(cache.latest)
			checkedAt := cache.checkedAt
			cache.mu.Unlock()
			return latest, DreamstarsReleaseSourceCached, checkedAt
		}
		if cache.inFlight != nil {
			inFlight := cache.inFlight
			cache.mu.Unlock()
			select {
			case <-inFlight:
				continue
			case <-ctx.Done():
				return nil, DreamstarsReleaseSourceUnavailable, now
			}
		}
		inFlight := make(chan struct{})
		cache.inFlight = inFlight
		cache.mu.Unlock()

		latest, err := dreamstarsReleaseFetch(ctx)
		cache.mu.Lock()
		cache.inFlight = nil
		close(inFlight)
		if err == nil {
			cache.latest = cloneDreamstarsRelease(latest)
			cache.checkedAt = now
			result := cloneDreamstarsRelease(cache.latest)
			checkedAt := cache.checkedAt
			cache.mu.Unlock()
			return result, DreamstarsReleaseSourceLive, checkedAt
		}
		if cache.latest != nil {
			result := cloneDreamstarsRelease(cache.latest)
			checkedAt := cache.checkedAt
			cache.mu.Unlock()
			return result, DreamstarsReleaseSourceStale, checkedAt
		}
		cache.mu.Unlock()
		return nil, DreamstarsReleaseSourceUnavailable, now
	}
}

func cloneDreamstarsRelease(release *DreamstarsRelease) *DreamstarsRelease {
	if release == nil {
		return nil
	}
	clone := *release
	clone.Platforms = append([]string(nil), release.Platforms...)
	return &clone
}

func fetchLatestDreamstarsRelease(ctx context.Context) (*DreamstarsRelease, error) {
	return fetchDreamstarsRelease(
		ctx,
		dreamstarsReleaseHTTPClient,
		dreamstarsReleaseGitHubReleasesURL,
		isTrustedDreamstarsReleaseAssetURL,
	)
}

func fetchDreamstarsRelease(
	ctx context.Context,
	client *http.Client,
	releasesURL string,
	assetURLValidator func(rawURL, releaseTag string) bool,
) (*DreamstarsRelease, error) {
	body, err := getDreamstarsReleaseJSON(ctx, client, releasesURL)
	if err != nil {
		return nil, err
	}
	var releases []dreamstarsGitHubRelease
	if err := common.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("decode Dreamstars GitHub releases: %w", err)
	}

	var selected *dreamstarsGitHubRelease
	for index := range releases {
		release := &releases[index]
		if release.Draft || release.Prerelease || !isDreamstarsReleaseVersion(release.TagName) {
			continue
		}
		selected = release
		break
	}
	if selected == nil {
		return nil, errors.New("no approved Dreamstars release found")
	}
	if selected.PublishedAt.IsZero() {
		return nil, errors.New("Dreamstars release has no publication time")
	}

	assetURL := ""
	for _, asset := range selected.Assets {
		if asset.Name == dreamstarsReleaseAssetName {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" || !assetURLValidator(assetURL, selected.TagName) {
		return nil, errors.New("Dreamstars release has no trusted manifest asset")
	}

	manifestBody, err := getDreamstarsReleaseJSON(ctx, client, assetURL)
	if err != nil {
		return nil, err
	}
	var manifest dreamstarsReleaseManifest
	if err := common.Unmarshal(manifestBody, &manifest); err != nil {
		return nil, fmt.Errorf("decode Dreamstars release manifest: %w", err)
	}
	if err := validateDreamstarsReleaseManifest(&manifest, selected.TagName); err != nil {
		return nil, err
	}

	return &DreamstarsRelease{
		ReleaseTag:  manifest.ReleaseTag,
		Version:     manifest.Version,
		Commit:      manifest.Commit,
		PublishedAt: selected.PublishedAt,
		ReleaseURL:  "https://github.com/MOON-DREAM-STARS/new-api/releases/tag/" + url.PathEscape(selected.TagName),
		Image:       manifest.Image,
		Digest:      manifest.Digest,
		Platforms:   append([]string(nil), manifest.Platforms...),
	}, nil
}

func getDreamstarsReleaseJSON(ctx context.Context, client *http.Client, requestURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "dreamstars-newapi-release-checker")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Dreamstars release request returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, dreamstarsReleaseResponseMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > dreamstarsReleaseResponseMaxBytes {
		return nil, errors.New("Dreamstars release response exceeds size limit")
	}
	return body, nil
}

func isTrustedDreamstarsReleaseAssetURL(rawURL, releaseTag string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") {
		return false
	}
	prefix := "/MOON-DREAM-STARS/new-api/releases/download/" + releaseTag + "/"
	return strings.HasPrefix(parsed.Path, prefix) && strings.HasSuffix(parsed.Path, "/"+dreamstarsReleaseAssetName)
}

func validateDreamstarsReleaseManifest(manifest *dreamstarsReleaseManifest, releaseTag string) error {
	if !isDreamstarsReleaseVersion(releaseTag) {
		return errors.New("Dreamstars release tag is invalid")
	}
	if manifest.SchemaVersion != 1 {
		return errors.New("Dreamstars release manifest schema is unsupported")
	}
	if manifest.ReleaseTag != releaseTag || manifest.Version != releaseTag {
		return errors.New("Dreamstars release manifest version does not match release tag")
	}
	if manifest.Repository != dreamstarsReleaseRepository || manifest.Branch != dreamstarsReleaseBranch {
		return errors.New("Dreamstars release manifest source is not trusted")
	}
	if !dreamstarsReleaseCommitPattern.MatchString(manifest.Commit) {
		return errors.New("Dreamstars release manifest commit is invalid")
	}
	if manifest.Image != dreamstarsReleaseImage || !dreamstarsReleaseDigestPattern.MatchString(manifest.Digest) {
		return errors.New("Dreamstars release manifest image identity is invalid")
	}
	if len(manifest.Platforms) != 1 || manifest.Platforms[0] != "linux/amd64" {
		return errors.New("Dreamstars release manifest platform is unsupported")
	}
	if manifest.PublishedAt.IsZero() {
		return errors.New("Dreamstars release manifest has no publication time")
	}
	if manifest.Signature.Type != dreamstarsReleaseSignatureType ||
		manifest.Signature.Issuer != dreamstarsReleaseSignatureIssuer ||
		manifest.Signature.Identity != dreamstarsReleaseSignatureIdentity {
		return errors.New("Dreamstars release manifest signature identity is invalid")
	}
	return nil
}

func isDreamstarsReleaseVersion(version string) bool {
	_, ok := parseDreamstarsReleaseVersion(version)
	return ok
}
