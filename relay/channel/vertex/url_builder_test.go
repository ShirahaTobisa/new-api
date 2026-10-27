package vertex

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildGoogleModelURL(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		projectID string
		region    string
		want      string
	}{
		{
			name:   "api key without base url",
			region: "global",
			want:   "https://aiplatform.googleapis.com/v1/publishers/google/models/m:generateContent",
		},
		{
			name:    "api key with host base url",
			baseURL: "https://aiplatform.googleapis.com",
			region:  "global",
			want:    "https://aiplatform.googleapis.com/v1/publishers/google/models/m:generateContent",
		},
		{
			name:      "service account with host base url",
			baseURL:   "https://aiplatform.googleapis.com",
			projectID: "p",
			region:    "global",
			want:      "https://aiplatform.googleapis.com/v1/projects/p/locations/global/publishers/google/models/m:generateContent",
		},
		{
			name:      "service account regional without base url",
			projectID: "p",
			region:    "us-central1",
			want:      "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models/m:generateContent",
		},
		{
			name:    "api key with full project path base url",
			baseURL: "https://aiplatform.googleapis.com/v1/projects/123/locations/global/",
			region:  "global",
			want:    "https://aiplatform.googleapis.com/v1/projects/123/locations/global/publishers/google/models/m:generateContent",
		},
		{
			name:      "full project path base url wins over credentials project",
			baseURL:   "https://aiplatform.googleapis.com/v1/projects/123/locations/global",
			projectID: "p",
			region:    "us-central1",
			want:      "https://aiplatform.googleapis.com/v1/projects/123/locations/global/publishers/google/models/m:generateContent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BuildGoogleModelURL(tt.baseURL, DefaultAPIVersion, tt.projectID, tt.region, "m", "generateContent"))
		})
	}
}

func TestBuildGoogleModelURLUsesTheLocationHost(t *testing.T) {
	tests := []struct {
		region string
		want   string
	}{
		{"global", "https://aiplatform.googleapis.com/v1/projects/my-project/locations/global/publishers/google/models/gemini-4-argon:generateContent"},
		{"us-central1", "https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/google/models/gemini-4-argon:generateContent"},
		{"us", "https://aiplatform.us.rep.googleapis.com/v1/projects/my-project/locations/us/publishers/google/models/gemini-4-argon:generateContent"},
		{"eu", "https://aiplatform.eu.rep.googleapis.com/v1/projects/my-project/locations/eu/publishers/google/models/gemini-4-argon:generateContent"},
	}
	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			got := BuildGoogleModelURL("", DefaultAPIVersion, "my-project", tt.region, "gemini-4-argon", "generateContent")
			assert.Equal(t, tt.want, got)
		})
	}
}
