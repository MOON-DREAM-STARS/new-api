// Package chatgpt classifies ChatGPT web URLs into provider resources.
//
// It is the single URL parsing authority of the Web Workspace provider
// adapter: the Browser Guard asks it whether one document navigation is a
// project, a conversation, an unknown provider shape or an unrelated path, and
// denies everything this package does not recognize. The frozen syntax only
// covers lower case ids, at most one trailing slash and no query or fragment;
// an escape that could hide structure inside a resource path is reported as
// unknown instead of being guessed.
package chatgpt

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

// Host is the provider host that carries ChatGPT resources. Subdomains of this
// host belong to the same provider, exactly like the address policy table.
const Host = "chatgpt.com"

// Kind is the classification of one ChatGPT URL.
type Kind string

const (
	// KindShell is the provider application shell ("/").
	KindShell Kind = "shell"
	// KindProject is /g/g-p-<id>[-<slug>] or /g/g-p-<id>[-<slug>]/project.
	KindProject Kind = "project"
	// KindConversation is /g/g-p-<id>[-<slug>]/c/<conversationId>.
	KindConversation Kind = "conversation"
	// KindConversationNoProject is /c/<conversationId>: a conversation that
	// carries no project, so it can never be owned. It is always denied.
	KindConversationNoProject Kind = "conversation_no_project"
	// KindUnknown is a /g/ or /c/ path outside the frozen syntax. It is always
	// denied, never guessed.
	KindUnknown Kind = "unknown"
	// KindOther is every other path of the provider hosts (auth, backend-api,
	// static assets, ...). It is not a provider resource.
	KindOther Kind = "other"
)

// Resource is one classified URL. Only the fields that belong to the kind are
// set: ProjectID is the external project id "g-p-<32 hex>", ConversationID the
// lower case conversation uuid and Slug the free form project slug.
type Resource struct {
	Kind           Kind
	ProjectID      string
	ConversationID string
	Slug           string
}

var (
	projectIDSuffix = regexp.MustCompile(`^[0-9a-f]{32}$`)
	conversationID  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// IsProviderHost reports whether host is the provider host or a subdomain of
// it. Hosts are compared case insensitively with a single trailing root label
// ignored, because that is how the address policy normalizes them as well.
func IsProviderHost(host string) bool {
	host = policy.NormalizeHost(host)
	return host == Host || strings.HasSuffix(host, "."+Host)
}

// Classify parses one request URL into the frozen ChatGPT syntax. An error
// means the URL itself could not be parsed and callers must deny it; every
// other URL is classified, with unknown as the fail closed shape.
func Classify(rawURL string) (Resource, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Resource{Kind: KindUnknown}, fmt.Errorf("parse chatgpt url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Resource{Kind: KindOther}, nil
	}
	if !IsProviderHost(parsed.Hostname()) {
		return Resource{Kind: KindOther}, nil
	}

	path := trimOneTrailingSlash(parsed.Path)
	rawPath := trimOneTrailingSlash(parsed.EscapedPath())
	if path == "" {
		path = "/"
	}
	if rawPath == "" {
		rawPath = "/"
	}
	// A backslash is not a separator in the frozen syntax, but some stacks turn
	// it into one. It must never pass as an unrelated path.
	if strings.ContainsRune(path, '\\') {
		return Resource{Kind: KindUnknown}, nil
	}
	if path == "/" {
		return Resource{Kind: KindShell}, nil
	}
	if !strings.HasPrefix(path, "/g/") && !strings.HasPrefix(path, "/c/") {
		return Resource{Kind: KindOther}, nil
	}

	// A resource path must not hide its structure in percent escapes: an
	// encoded separator or keyword would make the classifier and the provider
	// disagree about the very same URL. Escapes inside the free form project
	// slug stay allowed, everything else becomes unknown.
	decoded := strings.Split(path, "/")
	raw := strings.Split(rawPath, "/")
	if len(decoded) != len(raw) || decoded[1] != raw[1] {
		return Resource{Kind: KindUnknown}, nil
	}
	if decoded[1] == "c" {
		return classifyConversationWithoutProject(decoded, raw), nil
	}
	return classifyGroupPath(decoded, raw), nil
}

// classifyGroupPath handles every path below /g/.
func classifyGroupPath(decoded, raw []string) Resource {
	if len(decoded) < 3 {
		return Resource{Kind: KindUnknown}
	}
	projectID, slug, ok := parseProjectToken(decoded[2], raw[2])
	if !ok {
		return Resource{Kind: KindUnknown}
	}
	switch {
	case len(decoded) == 3:
		return Resource{Kind: KindProject, ProjectID: projectID, Slug: slug}
	case len(decoded) == 4 && decoded[3] == "project" && raw[3] == "project":
		return Resource{Kind: KindProject, ProjectID: projectID, Slug: slug}
	case len(decoded) == 5 && decoded[3] == "c" && raw[3] == "c" &&
		raw[4] == decoded[4] && conversationID.MatchString(decoded[4]):
		return Resource{Kind: KindConversation, ProjectID: projectID, ConversationID: decoded[4], Slug: slug}
	default:
		return Resource{Kind: KindUnknown}
	}
}

// classifyConversationWithoutProject handles /c/<conversationId>.
func classifyConversationWithoutProject(decoded, raw []string) Resource {
	if len(decoded) != 3 || raw[2] != decoded[2] || !conversationID.MatchString(decoded[2]) {
		return Resource{Kind: KindUnknown}
	}
	return Resource{Kind: KindConversationNoProject, ConversationID: decoded[2]}
}

// parseProjectToken splits one "g-p-<id>[-<slug>]" segment. The id is the text
// between the prefix and the first following dash: it must be exactly 32 lower
// case hex characters and byte identical before and after URL decoding.
func parseProjectToken(token, rawToken string) (string, string, bool) {
	const prefix = "g-p-"
	if !strings.HasPrefix(token, prefix) || !strings.HasPrefix(rawToken, prefix) {
		return "", "", false
	}
	rest := token[len(prefix):]
	id, slug := rest, ""
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		id, slug = rest[:i], rest[i+1:]
	}
	if !projectIDSuffix.MatchString(id) {
		return "", "", false
	}
	rawRest := rawToken[len(prefix):]
	rawID := rawRest
	if i := strings.IndexByte(rawRest, '-'); i >= 0 {
		rawID = rawRest[:i]
	}
	if rawID != id {
		return "", "", false
	}
	return prefix + id, slug, true
}

// trimOneTrailingSlash removes at most one trailing slash: the frozen syntax
// allows exactly one, so a second one stays part of the shape and makes the
// path unknown instead of being silently collapsed.
func trimOneTrailingSlash(path string) string {
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		return strings.TrimSuffix(path, "/")
	}
	return path
}
