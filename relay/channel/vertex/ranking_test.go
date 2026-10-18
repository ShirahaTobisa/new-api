package vertex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRankingURL(t *testing.T) {
	const rank = "https://discoveryengine.googleapis.com/v1/projects/%s/locations/global/rankingConfigs/default_ranking_config:rank"
	tests := []struct {
		name      string
		baseURL   string
		projectID string
		want      string
		wantErr   bool
	}{
		{name: "project from base url wins", baseURL: "https://aiplatform.googleapis.com/v1/projects/123/locations/global", projectID: "p", want: "123"},
		{name: "project from credentials", baseURL: "https://aiplatform.googleapis.com", projectID: "p", want: "p"},
		{name: "no project anywhere", baseURL: "https://aiplatform.googleapis.com", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildRankingURL(tt.baseURL, tt.projectID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, strings.Replace(rank, "%s", tt.want, 1), got)
		})
	}
}

func TestConvertRerankRequest(t *testing.T) {
	got := convertRerankRequest(dto.RerankRequest{
		Model:     "semantic-ranker-default@latest",
		Query:     "q",
		Documents: []any{"plain", map[string]any{"text": "wrapped"}},
		TopN:      lo.ToPtr(1),
	})
	assert.Equal(t, &RankingRequest{
		Model:                         "semantic-ranker-default@latest",
		Query:                         "q",
		Records:                       []RankingRecord{{ID: "0", Content: "plain"}, {ID: "1", Content: "wrapped"}},
		TopN:                          lo.ToPtr(1),
		IgnoreRecordDetailsInResponse: true,
	}, got)
}

func TestRankingHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	documents := make([]any, 150)
	for i := range documents {
		documents[i] = "doc"
	}
	documents[1] = "best"
	info := &relaycommon.RelayInfo{RerankerInfo: &relaycommon.RerankerInfo{Documents: documents, ReturnDocuments: true}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"records":[{"id":"1","score":0.9},{"id":"0","score":0.2},{"id":"999","score":0.1}]}`)),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	usage, apiErr := rankingHandler(c, resp, info)
	require.Nil(t, apiErr)

	// 150 documents are two 100-record queries on the GCP bill.
	assert.Equal(t, &dto.Usage{PromptTokens: 2, TotalTokens: 2}, usage)
	var body dto.RerankResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, []dto.RerankResponseResult{
		{Index: 1, RelevanceScore: 0.9, Document: "best"},
		{Index: 0, RelevanceScore: 0.2, Document: "doc"},
	}, body.Results)
	assert.Equal(t, *usage, body.Usage)
}
