package chatgpt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixtureCase struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Kind           string `json:"kind"`
	ProjectID      string `json:"project_id"`
	ConversationID string `json:"conversation_id"`
	Slug           string `json:"slug"`
	Deny           bool   `json:"deny"`
	Error          bool   `json:"error"`
}

func loadFixtures(t *testing.T) []fixtureCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "url_cases.json"))
	require.NoError(t, err)
	var fixture struct {
		Cases []fixtureCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture), "fixture is not valid JSON")
	return fixture.Cases
}

// TestClassifyFixtures drives the classifier from the frozen URL corpus. Every
// case pins the full classification, and the deny flag must follow from the
// kind so the corpus cannot accidentally document a fail open shape.
func TestClassifyFixtures(t *testing.T) {
	cases := loadFixtures(t)
	require.GreaterOrEqual(t, len(cases), 25, "the fixture must keep at least 25 cases")

	covered := map[Kind]bool{}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			resource, err := Classify(tc.URL)
			if tc.Error {
				require.Error(t, err, "unparsable URL must stay an error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, Kind(tc.Kind), resource.Kind)
			assert.Equal(t, tc.ProjectID, resource.ProjectID)
			assert.Equal(t, tc.ConversationID, resource.ConversationID)
			assert.Equal(t, tc.Slug, resource.Slug)

			wantDeny := resource.Kind == KindUnknown || resource.Kind == KindConversationNoProject
			assert.Equal(t, wantDeny, tc.Deny, "deny must follow from the classified kind")
		})
		covered[Kind(tc.Kind)] = true
	}

	for _, kind := range []Kind{KindShell, KindProject, KindConversation, KindConversationNoProject, KindUnknown, KindOther} {
		assert.True(t, covered[kind], "fixture must cover kind %q", kind)
	}
}

func TestIsProviderHost(t *testing.T) {
	for _, tc := range []struct {
		host  string
		match bool
	}{
		{host: "chatgpt.com", match: true},
		{host: "www.chatgpt.com", match: true},
		{host: "CHATGPT.com", match: true},
		{host: "chatgpt.com.", match: true},
		{host: "notchatgpt.com", match: false},
		{host: "chatgpt.com.evil.example", match: false},
		{host: "chat.openai.com", match: false},
		{host: "", match: false},
	} {
		t.Run(tc.host, func(t *testing.T) {
			assert.Equal(t, tc.match, IsProviderHost(tc.host))
		})
	}
}

// A resource id must never be guessed: everything outside the frozen syntax is
// unknown, which is the fail closed classification.
func TestClassifyNeverGuessesIDs(t *testing.T) {
	for _, rawURL := range []string{
		"https://chatgpt.com/g/g-p-0123456789abcdef0123456789abcde/project",
		"https://chatgpt.com/g/g-p-0123456789abcdef0123456789abcdef0/project",
		"https://chatgpt.com/g/g-p-0123456789abcdef0123456789abcdeF/project",
		"https://chatgpt.com/g/g-p-0123456789abcdef0123456789abcdef-extra/project/extra",
		"https://chatgpt.com/c/0f8fad5b-d9cb-469f-a165-70867728950",
	} {
		resource, err := Classify(rawURL)
		require.NoError(t, err)
		assert.Equal(t, KindUnknown, resource.Kind, rawURL)
		assert.Empty(t, resource.ProjectID)
		assert.Empty(t, resource.ConversationID)
	}
}
