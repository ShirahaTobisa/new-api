package vertex

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Vertex :predict embeds up to 250 texts per call with an API key or a service account;
// :embedContent (needed by gemini-embedding-2) takes one text and only OAuth (verified 2026-10-20).
type EmbeddingInstance struct {
	Content string `json:"content"`
}

type EmbeddingParameters struct {
	OutputDimensionality *int `json:"outputDimensionality,omitempty"`
}

type EmbeddingRequest struct {
	Instances  []EmbeddingInstance `json:"instances"`
	Parameters EmbeddingParameters `json:"parameters"`
}

type EmbeddingPrediction struct {
	Embeddings struct {
		Values     []float64 `json:"values"`
		Statistics struct {
			TokenCount int `json:"token_count"`
		} `json:"statistics"`
	} `json:"embeddings"`
}

type EmbeddingResponse struct {
	Predictions []EmbeddingPrediction `json:"predictions"`
}

func convertEmbeddingRequest(request dto.EmbeddingRequest) *EmbeddingRequest {
	inputs := request.ParseInput()
	instances := make([]EmbeddingInstance, len(inputs))
	for i, input := range inputs {
		instances[i] = EmbeddingInstance{Content: input}
	}
	return &EmbeddingRequest{
		Instances:  instances,
		Parameters: EmbeddingParameters{OutputDimensionality: request.Dimensions},
	}
}

func embeddingHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	var vertexResp EmbeddingResponse
	if err = common.Unmarshal(responseBody, &vertexResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	openAIResp := dto.OpenAIEmbeddingResponse{
		Object: "list",
		Data:   make([]dto.OpenAIEmbeddingResponseItem, len(vertexResp.Predictions)),
		Model:  info.UpstreamModelName,
	}
	promptTokens := 0
	for i, prediction := range vertexResp.Predictions {
		openAIResp.Data[i] = dto.OpenAIEmbeddingResponseItem{Object: "embedding", Index: i, Embedding: prediction.Embeddings.Values}
		promptTokens += max(prediction.Embeddings.Statistics.TokenCount, 0)
	}
	if promptTokens == 0 {
		promptTokens = info.GetEstimatePromptTokens()
	}
	openAIResp.Usage = dto.Usage{PromptTokens: promptTokens, TotalTokens: promptTokens}
	c.JSON(http.StatusOK, openAIResp)
	return &openAIResp.Usage, nil
}
