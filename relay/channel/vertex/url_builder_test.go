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
