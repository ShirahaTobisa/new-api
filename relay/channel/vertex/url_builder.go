package vertex

import (
	"fmt"
	"strings"
)

const (
	DefaultAPIVersion    = "v1"
	OpenSourceAPIVersion = "v1beta1"
	PublisherGoogle      = "google"
	PublisherAnthropic   = "anthropic"
)

func normalizeVertexBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func normalizeVertexRegion(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		return "global"
	}
	return region
}

func appendVertexAPIVersion(baseURL, version string) string {
	version = strings.Trim(strings.TrimSpace(version), "/")
	if version == "" {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/"+version) {
		return baseURL
	}
	return baseURL + "/" + version
}

func BuildAPIBaseURL(baseURL, version, projectID, region string) string {
	if normalized := normalizeVertexBaseURL(baseURL); normalized != "" {
		// 地址里已写明项目和地区（如 .../v1/projects/P/locations/global）就原样使用：
		// API Key 模式不带项目路径时，Google 会自行分配地区，可能落到模型没上线的地区
		if strings.Contains(normalized, "/projects/") {
			return normalized
		}
		normalized = appendVertexAPIVersion(normalized, version)

		region = normalizeVertexRegion(region)
		if strings.TrimSpace(projectID) != "" {
			normalized = fmt.Sprintf("%s/projects/%s/locations/%s", normalized, projectID, region)
		}
		return normalized
	}

	region = normalizeVertexRegion(region)
	host := region + "-aiplatform.googleapis.com"
	switch region {
	case "global":
		host = "aiplatform.googleapis.com"
	case "us", "eu":
		// The US and EU multi-regions are served from their own hostnames.
		host = "aiplatform." + region + ".rep.googleapis.com"
	}
	if strings.TrimSpace(projectID) == "" {
		return fmt.Sprintf("https://%s/%s", host, version)
	}
	return fmt.Sprintf("https://%s/%s/projects/%s/locations/%s", host, version, projectID, region)
}

func BuildPublisherModelURL(baseURL, version, projectID, region, publisher, modelName, action string) string {
	return fmt.Sprintf(
		"%s/publishers/%s/models/%s:%s",
		BuildAPIBaseURL(baseURL, version, projectID, region),
		publisher,
		modelName,
		action,
	)
}

func BuildGoogleModelURL(baseURL, version, projectID, region, modelName, action string) string {
	return BuildPublisherModelURL(baseURL, version, projectID, region, PublisherGoogle, modelName, action)
}

func BuildAnthropicModelURL(baseURL, version, projectID, region, modelName, action string) string {
	return BuildPublisherModelURL(baseURL, version, projectID, region, PublisherAnthropic, modelName, action)
}

func BuildOpenSourceChatCompletionsURL(baseURL, projectID, region string) string {
	return fmt.Sprintf(
		"%s/endpoints/openapi/chat/completions",
		BuildAPIBaseURL(baseURL, OpenSourceAPIVersion, projectID, region),
	)
}
