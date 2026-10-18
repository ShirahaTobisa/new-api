package vertex

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// GCP Ranking API (Discovery Engine) bills one query per 100 records.
const rankingRecordsPerQuery = 100

type RankingRecord struct {
	ID      string  `json:"id"`
	Content string  `json:"content,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

type RankingRequest struct {
	Model                         string          `json:"model"`
	Query                         string          `json:"query"`
	Records                       []RankingRecord `json:"records"`
	TopN                          *int            `json:"topN,omitempty"`
	IgnoreRecordDetailsInResponse bool            `json:"ignoreRecordDetailsInResponse"`
}

type RankingResponse struct {
	Records []RankingRecord `json:"records"`
}

var baseURLProjectID = regexp.MustCompile(`/projects/([^/]+)`)

// BuildRankingURL takes the project from a channel address that spells it out
// (.../projects/P/locations/...), otherwise from the service-account credentials.
func BuildRankingURL(baseURL, projectID string) (string, error) {
	if m := baseURLProjectID.FindStringSubmatch(baseURL); m != nil {
		projectID = m[1]
	}
	if projectID == "" {
		return "", errors.New("vertex rerank needs a project id: put /projects/{id}/locations/global in the channel address or use a service-account key")
	}
	return fmt.Sprintf("https://discoveryengine.googleapis.com/v1/projects/%s/locations/global/rankingConfigs/default_ranking_config:rank", projectID), nil
}

func convertRerankRequest(request dto.RerankRequest) *RankingRequest {
	records := make([]RankingRecord, len(request.Documents))
	for i, doc := range request.Documents {
		content := fmt.Sprint(doc)
		switch v := doc.(type) {
		case string:
			content = v
		case map[string]any:
			if text, ok := v["text"].(string); ok {
				content = text
			}
		}
		records[i] = RankingRecord{ID: strconv.Itoa(i), Content: content}
	}
	return &RankingRequest{
		Model:                         request.Model,
		Query:                         request.Query,
		Records:                       records,
		TopN:                          request.TopN,
		IgnoreRecordDetailsInResponse: true,
	}
}

func rankingHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	var rankingResp RankingResponse
	if err = common.Unmarshal(responseBody, &rankingResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	results := make([]dto.RerankResponseResult, 0, len(rankingResp.Records))
	for _, record := range rankingResp.Records {
		index, err := strconv.Atoi(record.ID)
		if err != nil || index < 0 || index >= len(info.Documents) {
			continue
		}
		result := dto.RerankResponseResult{Index: index, RelevanceScore: record.Score}
		if info.ReturnDocuments {
			result.Document = info.Documents[index]
		}
		results = append(results, result)
	}
	queries := (len(info.Documents) + rankingRecordsPerQuery - 1) / rankingRecordsPerQuery
	rerankResp := dto.RerankResponse{
		Results: results,
		Usage:   dto.Usage{PromptTokens: queries, TotalTokens: queries},
	}
	c.JSON(http.StatusOK, rerankResp)
	return &rerankResp.Usage, nil
}
