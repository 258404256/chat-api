package openai

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"one-api/common"
	"one-api/common/config"
	"one-api/relay/channel"
	"one-api/relay/channel/minimax"
	"one-api/relay/constant"
	"one-api/relay/model"
	"one-api/relay/util"

	"strings"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	ChannelType int
}

func (a *Adaptor) Init(meta *util.RelayMeta) {
	a.ChannelType = meta.ChannelType
}

func (a *Adaptor) GetRequestURL(meta *util.RelayMeta) (string, error) {
	switch meta.ChannelType {
	case common.ChannelTypeAzure:
		if meta.Mode == constant.RelayModeImagesGenerations {
			// https://learn.microsoft.com/en-us/azure/ai-services/openai/dall-e-quickstart?tabs=dalle3%2Ccommand-line&pivots=rest-api
			// https://{resource_name}.openai.azure.com/openai/deployments/dall-e-3/images/generations?api-version=2024-03-01-preview
			fullRequestURL := fmt.Sprintf("%s/openai/deployments/%s/images/generations?api-version=%s", meta.BaseURL, meta.ActualModelName, meta.Config.APIVersion)
			return fullRequestURL, nil
		}

		// https://learn.microsoft.com/en-us/azure/cognitive-services/openai/chatgpt-quickstart?pivots=rest-api&tabs=command-line#rest-api
		requestURL := strings.Split(meta.RequestURLPath, "?")[0]
		requestURL = fmt.Sprintf("%s?api-version=%s", requestURL, meta.Config.APIVersion)
		task := strings.TrimPrefix(requestURL, "/v1/")
		model_ := meta.ActualModelName
		model_ = strings.Replace(model_, ".", "", -1)
		model_ = strings.TrimSuffix(model_, "-0301")
		model_ = strings.TrimSuffix(model_, "-0314")
		model_ = strings.TrimSuffix(model_, "-0613")
		if meta.ActualModelName == "gpt-4-turbo" {
			model_ = "gpt-4-turbo-2024-04-09"
		}
		if meta.ActualModelName == "gpt-4o-2024-05-13" {
			model_ = "gpt-4o"
		}
		//https://github.com/songquanpeng/one-api/issues/1191
		// {your endpoint}/openai/deployments/{your azure_model}/chat/completions?api-version={api_version}
		requestURL = fmt.Sprintf("/openai/deployments/%s/%s", model_, task)
		return util.GetFullRequestURL(meta.BaseURL, requestURL, meta.ChannelType), nil
	case common.ChannelTypeMinimax:
		return minimax.GetRequestURL(meta)
	case common.ChannelTypeDouBao:
		return fmt.Sprintf("%s/api/v3/chat/completions", meta.BaseURL), nil
	case common.ChannelTypeCustom:
		return meta.BaseURL, nil
	default:
		return util.GetFullRequestURL(meta.BaseURL, meta.RequestURLPath, meta.ChannelType), nil
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Request, meta *util.RelayMeta) error {
	channel.SetupCommonRequestHeader(c, req, meta)
	if meta.ChannelType == common.ChannelTypeAzure {
		req.Header.Set("api-key", meta.APIKey)
		return nil
	} else if meta.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+meta.APIKey)
		return nil
	}
	if meta.ChannelType == common.ChannelTypeOpenRouter {
		req.Header.Set("X-Title", "One API")
	}
	return nil
}

func (a *Adaptor) ConvertRequest(c *gin.Context, relayMode int, request *model.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, meta *util.RelayMeta, requestBody io.Reader) (*http.Response, error) {
	return channel.DoRequestHelper(a, c, meta, requestBody)

}

func (a *Adaptor) ConvertImageRequest(request *model.ImageRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, meta *util.RelayMeta) (aitext string, usage *model.Usage, err *model.ErrorWithStatusCode) {

	aitext = ""
	if meta.IsStream {

		var responseText string
		var toolCount int
		if meta.FixedContent != "" {
			err, responseText, toolCount = StreamHandler(c, resp, meta.Mode, meta.ActualModelName, meta.FixedContent)
		} else {
			err, responseText, toolCount = StreamHandler(c, resp, meta.Mode, meta.ActualModelName, meta.FixedContent)
		}

		aitext = responseText
		if usage == nil || usage.TotalTokens == 0 {
			usage = ResponseText2Usage(responseText, meta.ActualModelName, meta.PromptTokens)
		}
		if usage.TotalTokens != 0 && usage.PromptTokens == 0 { // some channels don't return prompt tokens & completion tokens
			usage.PromptTokens = meta.PromptTokens
			usage.CompletionTokens = usage.TotalTokens - meta.PromptTokens
		}
		usage.CompletionTokens += toolCount * 7
		if usage.CompletionTokens == 0 {
			if config.BlankReplyRetryEnabled &&
				!strings.HasPrefix(meta.OriginModelName, "tts") &&
				!strings.HasPrefix(meta.OriginModelName, "whisper-1") &&
				!strings.HasPrefix(meta.OriginModelName, "text") {
				return "", nil, &model.ErrorWithStatusCode{
					Error: model.Error{
						Message: "No completion tokens generated",
						Type:    "chat_api_error",
						Param:   "completionTokens",
						Code:    500,
					},
					StatusCode: 500,
				}
			}
		}

	} else {
		switch meta.Mode {
		case constant.RelayModeImagesGenerations:
			err, _ = ImageHandler(c, resp)
		case constant.RelayModeEdits:
			err, _ = ImagesEditsHandler(c, resp)
		default:
			err, usage, aitext = Handler(c, resp, meta.PromptTokens, meta.ActualModelName)
		}

	}
	return
}

func (a *Adaptor) GetModelList() []string {
	_, modelList := GetCompatibleChannelMeta(a.ChannelType)
	return modelList
}

func (a *Adaptor) GetChannelName() string {
	channelName, _ := GetCompatibleChannelMeta(a.ChannelType)
	return channelName
}
