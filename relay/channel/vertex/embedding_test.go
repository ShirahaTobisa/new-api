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

func TestConvertEmbeddingRequest(t *testing.T) {
	batch, err := common.Marshal(convertEmbeddingRequest(dto.EmbeddingRequest{Input: []any{"a", "b"}, Dimensions: lo.ToPtr(8)}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"instances":[{"content":"a"},{"content":"b"}],"parameters":{"outputDimensionality":8}}`, string(batch))

	single, err := common.Marshal(convertEmbeddingRequest(dto.EmbeddingRequest{Input: "a"}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"instances":[{"content":"a"}],"parameters":{}}`, string(single))
}

func TestEmbeddingHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		body       string
		wantTokens int
	}{
		{
			name:       "usage from upstream token counts",
			body:       `{"predictions":[{"embeddings":{"values":[0.1,0.2],"statistics":{"token_count":3}}},{"embeddings":{"values":[0.3,0.4],"statistics":{"token_count":4}}}],"metadata":{"billableCharacterCount":10}}`,
			wantTokens: 7,
		},
		{
			name:       "estimate when upstream omits counts",
			body:       `{"predictions":[{"embeddings":{"values":[0.1,0.2]}},{"embeddings":{"values":[0.3,0.4]}}]}`,
			wantTokens: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-embedding-001"}}
			info.SetEstimatePromptTokens(5)
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.body))}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)

			usage, apiErr := embeddingHandler(c, resp, info)
			require.Nil(t, apiErr)

			assert.Equal(t, &dto.Usage{PromptTokens: tt.wantTokens, TotalTokens: tt.wantTokens}, usage)
			var body dto.OpenAIEmbeddingResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, []dto.OpenAIEmbeddingResponseItem{
				{Object: "embedding", Index: 0, Embedding: []float64{0.1, 0.2}},
				{Object: "embedding", Index: 1, Embedding: []float64{0.3, 0.4}},
			}, body.Data)
			assert.Equal(t, "gemini-embedding-001", body.Model)
			assert.Equal(t, *usage, body.Usage)
		})
	}
}
