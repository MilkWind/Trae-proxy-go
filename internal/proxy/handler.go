package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"trae-proxy-go/internal/logger"
	"trae-proxy-go/pkg/models"
)

// Handler 处理器结构
type Handler struct {
	config *models.Config
	logger *logger.Logger
}

// NewHandler 创建新的处理器
func NewHandler(config *models.Config, logger *logger.Logger) *Handler {
	return &Handler{
		config: config,
		logger: logger,
	}
}

// HandleRoot 处理根路径
func (h *Handler) HandleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]string{
		"message": "Welcome to the Trae Proxy",
	}
	h.writeJSON(w, response)
}

// HandleV1Root 处理/v1路径
func (h *Handler) HandleV1Root(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"message": "OpenAI API v1 endpoint",
		"endpoints": map[string]string{
			"chat/completions": "/v1/chat/completions",
			"responses":        "/v1/responses",
			"messages":         "/v1/messages",
		},
	}
	h.writeJSON(w, response)
}

func (h *Handler) buildAnthropicModelResponse(api *models.API) map[string]interface{} {
	return map[string]interface{}{
		"id":           api.CustomModelID,
		"created_at":   "2021-07-20T10:40:00Z",
		"display_name": api.CustomModelID,
		"type":         "model",
	}
}

func (h *Handler) findAnthropicAPIByModelID(modelID string) *models.API {
	for i := range h.config.APIs {
		api := &h.config.APIs[i]
		if api.Active && api.Format == "anthropic" && api.CustomModelID == modelID {
			return api
		}
	}
	return nil
}

func (h *Handler) findResponsesAPIByModelID(modelID string) *models.API {
	for i := range h.config.APIs {
		api := &h.config.APIs[i]
		if api.Active && api.Format == "responses" && api.CustomModelID == modelID {
			return api
		}
	}
	return nil
}

// HandleModels 处理模型列表请求
func (h *Handler) HandleModels(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("handling models")
	if h.logger != nil {
		h.logger.Info("models 请求方法: %s", r.Method)
		h.logger.Info("models 请求路径: %s", r.URL.String())
		h.logger.Info("models 请求Host: %s", r.Host)
		h.logger.Info("models 请求头: %v", r.Header)
		h.logger.Info("models 查询参数: %v", r.URL.Query())
	}
	if r.Method != http.MethodGet {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 尝试判断是否是 Anthropic 格式的请求（可以根据特定的 header 或 Host 来判断）
	isAnthropic := false
	if r.Header.Get("x-api-key") != "" || r.Header.Get("anthropic-version") != "" || r.Host == "api.anthropic.com" {
		isAnthropic = true
	}
	h.logger.Info("handling models two")
	if isAnthropic {
		h.logger.Info("handling models three")
		models := []map[string]interface{}{}
		for _, api := range h.config.APIs {
			if api.Active && api.Format == "anthropic" {
				models = append(models, h.buildAnthropicModelResponse(&api))
			}
		}

		response := map[string]interface{}{
			"data":     models,
			"has_more": false,
		}
		if len(models) > 0 {
			response["first_id"] = models[0]["id"]
			response["last_id"] = models[len(models)-1]["id"]
		} else {
			response["first_id"] = ""
			response["last_id"] = ""
		}
		if responseJSON, err := json.Marshal(response); err == nil {
			h.logger.Info("models 响应JSON: %s", string(responseJSON))
		} else {
			h.logger.Error("序列化 models 响应日志失败: %v", err)
		}

		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
		}
		w.Header().Set("Request-Id", requestID)
		w.Header().Set("anthropic-version", r.Header.Get("anthropic-version"))
		w.Header().Set("Server", "envoy")

		h.writeJSON(w, response)
		return
	}
	h.logger.Info("handling models four")
	// 默认返回 OpenAI 格式模型列表（包含 openai 与 responses 两类）
	models := []map[string]interface{}{}
	for _, api := range h.config.APIs {
		if api.Active && (api.Format == "" || api.Format == "openai" || api.Format == "responses") {
			models = append(models, map[string]interface{}{
				"id":       api.CustomModelID,
				"object":   "model",
				"created":  1,
				"owned_by": "trae-proxy",
			})
		}
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   models,
	}
	h.writeJSON(w, response)
}

// HandleModelByID 处理单个模型详情请求
func (h *Handler) HandleModelByID(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("handling model by id")
	if r.Method != http.MethodGet {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if modelID == "" || strings.Contains(modelID, "/") {
		http.NotFound(w, r)
		return
	}

	if responsesAPI := h.findResponsesAPIByModelID(modelID); responsesAPI != nil {
		h.writeJSON(w, map[string]interface{}{
			"id":       responsesAPI.CustomModelID,
			"object":   "model",
			"created":  1,
			"owned_by": "trae-proxy",
		})
		return
	}

	api := h.findAnthropicAPIByModelID(modelID)
	if api == nil {
		http.NotFound(w, r)
		return
	}

	h.writeJSON(w, h.buildAnthropicModelResponse(api))
}

// HandleChatCompletions 处理聊天完成请求
func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 检查Content-Type
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		h.writeError(w, "Content-Type必须为application/json", http.StatusBadRequest)
		return
	}

	// 解析请求体
	var reqJSON map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqJSON); err != nil {
		h.writeError(w, fmt.Sprintf("无效的JSON请求体: %v", err), http.StatusBadRequest)
		return
	}

	// 调试日志
	if h.logger != nil {
		h.logger.Debug("请求头: %v", r.Header)
		reqJSONBytes, _ := json.Marshal(reqJSON)
		h.logger.Debug("请求体: %s", string(reqJSONBytes))
	}

	// 获取请求的模型ID
	requestedModel, _ := reqJSON["model"].(string)

	// 选择后端API
	selectedBackend := selectBackendByModel(h.config, requestedModel)
	if selectedBackend == nil {
		h.writeError(w, "未找到可用的后端API配置", http.StatusInternalServerError)
		return
	}

	targetAPIURL := selectedBackend.Endpoint
	targetModelID := selectedBackend.TargetModelID
	customModelID := selectedBackend.CustomModelID
	streamMode := selectedBackend.StreamMode

	if h.logger != nil {
		h.logger.Info("选择后端: %s -> %s", selectedBackend.Name, targetAPIURL)
	}

	// 修改模型ID
	reqJSON["model"] = targetModelID

	// 处理流模式
	// streamMode: "true" 强制开启, "false" 强制关闭, "" 或不设置则保持原请求设置
	if streamMode == "true" {
		reqJSON["stream"] = true
	} else if streamMode == "false" {
		reqJSON["stream"] = false
	}
	// 如果streamMode为空，保持原请求的stream设置（不修改）

	// 准备转发请求
	reqBody, err := json.Marshal(reqJSON)
	if err != nil {
		h.writeError(w, fmt.Sprintf("序列化请求失败: %v", err), http.StatusInternalServerError)
		return
	}

	targetURL := fmt.Sprintf("%s/v1/chat/completions", targetAPIURL)
	if h.logger != nil {
		h.logger.Debug("转发请求到: %s", targetURL)
	}

	// 创建转发请求
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewBuffer(reqBody))
	if err != nil {
		h.writeError(w, fmt.Sprintf("创建请求失败: %v", err), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if selectedBackend.CustomAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+selectedBackend.CustomAPIKey)
	} else if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("请求失败: %v", err)
		}
		h.writeError(w, fmt.Sprintf("请求异常: %v", err), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	// 处理错误响应
	if resp.StatusCode >= 400 {
		errorBody, _ := io.ReadAll(resp.Body)
		var errorJSON map[string]interface{}
		if err := json.Unmarshal(errorBody, &errorJSON); err == nil {
			h.writeJSON(w, errorJSON, resp.StatusCode)
		} else {
			h.writeError(w, fmt.Sprintf("HTTP错误: %s", resp.Status), resp.StatusCode)
		}
		return
	}

	// 检查是否为流式响应
	isStream, _ := reqJSON["stream"].(bool)
	if isStream {
		// 流式响应
		if h.logger != nil {
			h.logger.Debug("返回流式响应")
		}
		if err := StreamResponse(w, resp.Body, customModelID); err != nil {
			if h.logger != nil {
				h.logger.Error("流式响应处理失败: %v", err)
			}
		}
		return
	}

	// 非流式响应
	var responseJSON map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseJSON); err != nil {
		h.writeError(w, fmt.Sprintf("解析响应失败: %v", err), http.StatusInternalServerError)
		return
	}

	if h.logger != nil {
		responseJSONBytes, _ := json.Marshal(responseJSON)
		h.logger.Debug("响应体: %s", string(responseJSONBytes))
	}

	// 修改响应中的模型ID
	if responseJSON["model"] != nil {
		responseJSON["model"] = customModelID
	}

	h.writeJSON(w, responseJSON)
}

func buildMessagesFromResponsesInput(inputData interface{}) []interface{} {
	messages := make([]interface{}, 0)

	appendMessage := func(role string, content interface{}) {
		message := map[string]interface{}{
			"role":    role,
			"content": content,
		}
		messages = append(messages, message)
	}

	appendFromParts := func(role string, parts []interface{}) {
		textParts := make([]string, 0)
		for _, p := range parts {
			part, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			partType, _ := part["type"].(string)
			switch partType {
			case "input_text", "output_text", "text":
				if txt, ok := part["text"].(string); ok && txt != "" {
					textParts = append(textParts, txt)
				}
			}
		}
		if len(textParts) > 0 {
			appendMessage(role, strings.Join(textParts, "\n"))
		}
	}

	switch v := inputData.(type) {
	case string:
		if v != "" {
			appendMessage("user", v)
		}
	case []interface{}:
		for _, item := range v {
			switch iv := item.(type) {
			case string:
				if iv != "" {
					appendMessage("user", iv)
				}
			case map[string]interface{}:
				role, _ := iv["role"].(string)
				if role == "" {
					role = "user"
				}
				if itemType, _ := iv["type"].(string); itemType == "message" {
					if contentArr, ok := iv["content"].([]interface{}); ok {
						appendFromParts(role, contentArr)
						continue
					}
				}
				if content, ok := iv["content"]; ok {
					switch cv := content.(type) {
					case string:
						if cv != "" {
							appendMessage(role, cv)
						}
					case []interface{}:
						appendFromParts(role, cv)
					}
				}
			}
		}
	}

	return messages
}

func convertResponsesToChatRequest(reqJSON map[string]interface{}) map[string]interface{} {
	ccReq := map[string]interface{}{
		"model":  reqJSON["model"],
		"stream": reqJSON["stream"],
	}

	messages := make([]interface{}, 0)

	if instructions, ok := reqJSON["instructions"].(string); ok && instructions != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": instructions,
		})
	}

	if inputData, ok := reqJSON["input"]; ok {
		messages = append(messages, buildMessagesFromResponsesInput(inputData)...)
	}
	ccReq["messages"] = messages

	if tools, ok := reqJSON["tools"]; ok {
		ccReq["tools"] = tools
	}
	if temperature, ok := reqJSON["temperature"]; ok {
		ccReq["temperature"] = temperature
	}
	if topP, ok := reqJSON["top_p"]; ok {
		ccReq["top_p"] = topP
	}
	if maxOutputTokens, ok := reqJSON["max_output_tokens"]; ok {
		ccReq["max_tokens"] = maxOutputTokens
	}
	if toolChoice, ok := reqJSON["tool_choice"]; ok {
		ccReq["tool_choice"] = toolChoice
	}

	return ccReq
}

func convertChatResponseToResponses(responseJSON map[string]interface{}, customModelID string) map[string]interface{} {
	resp := map[string]interface{}{
		"id":     responseJSON["id"],
		"object": "response",
		"status": "completed",
		"model":  customModelID,
	}

	usage := map[string]interface{}{
		"input_tokens":  0,
		"output_tokens": 0,
		"total_tokens":  0,
	}
	if rawUsage, ok := responseJSON["usage"].(map[string]interface{}); ok {
		if v, ok := rawUsage["prompt_tokens"]; ok {
			usage["input_tokens"] = v
		}
		if v, ok := rawUsage["completion_tokens"]; ok {
			usage["output_tokens"] = v
		}
		if v, ok := rawUsage["total_tokens"]; ok {
			usage["total_tokens"] = v
		}
	}
	resp["usage"] = usage

	output := make([]interface{}, 0)
	if choices, ok := responseJSON["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if finishReason, ok := choice["finish_reason"].(string); ok && finishReason == "length" {
				resp["status"] = "incomplete"
			}
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if reasoning, ok := message["reasoning_content"].(string); ok && reasoning != "" {
					output = append(output, map[string]interface{}{
						"type": "reasoning",
						"id":   "rs_from_chat",
						"summary": []interface{}{
							map[string]interface{}{
								"type": "summary_text",
								"text": reasoning,
							},
						},
					})
				}

				if content, ok := message["content"].(string); ok && content != "" {
					output = append(output, map[string]interface{}{
						"type":   "message",
						"id":     "msg_from_chat",
						"status": "completed",
						"role":   "assistant",
						"content": []interface{}{
							map[string]interface{}{
								"type": "output_text",
								"text": content,
							},
						},
					})
				}

				if toolCalls, ok := message["tool_calls"].([]interface{}); ok {
					for _, tc := range toolCalls {
						toolCall, ok := tc.(map[string]interface{})
						if !ok {
							continue
						}
						functionData, _ := toolCall["function"].(map[string]interface{})
						name, _ := functionData["name"].(string)
						arguments, _ := functionData["arguments"].(string)
						callID, _ := toolCall["id"].(string)
						if callID == "" {
							callID = "call_from_chat"
						}
						output = append(output, map[string]interface{}{
							"type":      "function_call",
							"id":        "fc_" + callID,
							"status":    "completed",
							"call_id":   callID,
							"name":      name,
							"arguments": arguments,
						})
					}
				}
			}
		}
	}
	resp["output"] = output
	return resp
}

// HandleResponses 处理 Cursor/OpenAI Responses 格式请求
func (h *Handler) HandleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		h.writeError(w, "Content-Type必须为application/json", http.StatusBadRequest)
		return
	}

	var reqJSON map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqJSON); err != nil {
		h.writeError(w, fmt.Sprintf("无效的JSON请求体: %v", err), http.StatusBadRequest)
		return
	}

	requestedModel, _ := reqJSON["model"].(string)
	selectedBackend := selectBackendByModel(h.config, requestedModel)
	if selectedBackend == nil {
		h.writeError(w, "未找到可用的后端API配置", http.StatusInternalServerError)
		return
	}

	targetAPIURL := selectedBackend.Endpoint
	targetModelID := selectedBackend.TargetModelID
	customModelID := selectedBackend.CustomModelID
	streamMode := selectedBackend.StreamMode

	if h.logger != nil {
		h.logger.Info("Responses 选择后端: %s -> %s", selectedBackend.Name, targetAPIURL)
	}

	reqJSON["model"] = targetModelID

	if streamMode == "true" {
		reqJSON["stream"] = true
	} else if streamMode == "false" {
		reqJSON["stream"] = false
	}

	targetURL := fmt.Sprintf("%s/v1/responses", targetAPIURL)
	requestBody := reqJSON
	backendFormat := selectedBackend.Format
	switch backendFormat {
	case "responses":
		// pass-through
	case "openai", "":
		targetURL = fmt.Sprintf("%s/v1/chat/completions", targetAPIURL)
		requestBody = convertResponsesToChatRequest(reqJSON)
	case "anthropic":
		h.writeError(w, "当前版本尚不支持 /v1/responses 直接桥接到 anthropic 后端", http.StatusBadRequest)
		return
	default:
		// unknown format defaults to /v1/responses for compatibility
	}

	reqBody, err := json.Marshal(requestBody)
	if err != nil {
		h.writeError(w, fmt.Sprintf("序列化请求失败: %v", err), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewBuffer(reqBody))
	if err != nil {
		h.writeError(w, fmt.Sprintf("创建请求失败: %v", err), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if selectedBackend.CustomAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+selectedBackend.CustomAPIKey)
	} else if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	if apiKey := r.Header.Get("x-api-key"); apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	if openAIKey := r.Header.Get("OpenAI-Beta"); openAIKey != "" {
		req.Header.Set("OpenAI-Beta", openAIKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("Responses 请求失败: %v", err)
		}
		h.writeError(w, fmt.Sprintf("请求异常: %v", err), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errorBody, _ := io.ReadAll(resp.Body)
		var errorJSON map[string]interface{}
		if err := json.Unmarshal(errorBody, &errorJSON); err == nil {
			h.writeJSON(w, errorJSON, resp.StatusCode)
		} else {
			h.writeError(w, fmt.Sprintf("HTTP错误: %s", resp.Status), resp.StatusCode)
		}
		return
	}

	isStream, _ := requestBody["stream"].(bool)
	if isStream {
		if err := StreamResponse(w, resp.Body, customModelID); err != nil {
			if h.logger != nil {
				h.logger.Error("Responses 流式响应处理失败: %v", err)
			}
		}
		return
	}

	var responseJSON map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseJSON); err != nil {
		h.writeError(w, fmt.Sprintf("解析响应失败: %v", err), http.StatusInternalServerError)
		return
	}

	if backendFormat == "openai" || backendFormat == "" {
		h.writeJSON(w, convertChatResponseToResponses(responseJSON, customModelID))
		return
	}

	if responseJSON["model"] != nil {
		responseJSON["model"] = customModelID
	}
	h.writeJSON(w, responseJSON)
}

// HandleMessages 处理 Claude 格式的请求
func (h *Handler) HandleMessages(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("claude格式")
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 检查Content-Type
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		h.writeError(w, "Content-Type必须为application/json", http.StatusBadRequest)
		return
	}

	// 解析请求体
	var reqJSON map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqJSON); err != nil {
		h.writeError(w, fmt.Sprintf("无效的JSON请求体: %v", err), http.StatusBadRequest)
		return
	}

	// 调试日志
	if h.logger != nil {
		reqJSONBytes, _ := json.Marshal(reqJSON)
		h.logger.Debug("Claude请求头: %v", r.Header)
		h.logger.Debug("Claude请求体: %s", string(reqJSONBytes))
	}

	// 获取请求的模型ID
	requestedModel, _ := reqJSON["model"].(string)

	// 选择后端API
	selectedBackend := selectBackendByModel(h.config, requestedModel)
	if selectedBackend == nil {
		h.writeError(w, "未找到可用的后端API配置", http.StatusInternalServerError)
		return
	}

	targetAPIURL := selectedBackend.Endpoint
	targetModelID := selectedBackend.TargetModelID
	customModelID := selectedBackend.CustomModelID
	streamMode := selectedBackend.StreamMode

	if h.logger != nil {
		h.logger.Info("Claude 选择后端: %s -> %s", selectedBackend.Name, targetAPIURL)
	}

	// 修改模型ID
	reqJSON["model"] = targetModelID

	// 处理流模式
	if streamMode == "true" {
		reqJSON["stream"] = true
	} else if streamMode == "false" {
		reqJSON["stream"] = false
	}

	// 准备转发请求
	reqBody, err := json.Marshal(reqJSON)
	if err != nil {
		h.writeError(w, fmt.Sprintf("序列化请求失败: %v", err), http.StatusInternalServerError)
		return
	}

	// Anthropic 的转发路径一般是 /v1/messages
	targetURL := fmt.Sprintf("%s/v1/messages", targetAPIURL)
	if h.logger != nil {
		h.logger.Debug("转发 Claude 请求到: %s", targetURL)
	}

	// 创建转发请求
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewBuffer(reqBody))
	if err != nil {
		h.writeError(w, fmt.Sprintf("创建请求失败: %v", err), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	// 透传 Claude 相关的特定头部
	if selectedBackend.CustomAPIKey != "" {
		req.Header.Set("x-api-key", selectedBackend.CustomAPIKey)
	} else if apiKey := r.Header.Get("x-api-key"); apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	if anthropicVers := r.Header.Get("anthropic-version"); anthropicVers != "" {
		req.Header.Set("anthropic-version", anthropicVers)
	}

	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		// 这里处理 Authorization
		if selectedBackend.CustomAPIKey != "" && len(authHeader) > 7 {
			req.Header.Set("Authorization", "Bearer "+selectedBackend.CustomAPIKey)
		} else {
			req.Header.Set("Authorization", authHeader)
		}
	} else if selectedBackend.CustomAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+selectedBackend.CustomAPIKey)
	}

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("Claude 请求失败: %v", err)
		}
		h.writeError(w, fmt.Sprintf("请求异常: %v", err), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	// 错误反馈
	if resp.StatusCode >= 400 {
		errorBody, _ := io.ReadAll(resp.Body)
		var errorJSON map[string]interface{}
		if err := json.Unmarshal(errorBody, &errorJSON); err == nil {
			h.writeJSON(w, errorJSON, resp.StatusCode)
		} else {
			h.writeError(w, fmt.Sprintf("HTTP错误: %s", resp.Status), resp.StatusCode)
		}
		return
	}

	// 检查是否为流式响应
	isStream, _ := reqJSON["stream"].(bool)
	if isStream {
		if h.logger != nil {
			h.logger.Debug("返回 Claude 流式响应")
		}
		if err := StreamResponse(w, resp.Body, customModelID); err != nil {
			if h.logger != nil {
				h.logger.Error("流式响应处理失败: %v", err)
			}
		}
		return
	}

	// 非流式响应
	var responseJSON map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseJSON); err != nil {
		h.writeError(w, fmt.Sprintf("解析响应失败: %v", err), http.StatusInternalServerError)
		return
	}

	if h.logger != nil {
		responseJSONBytes, _ := json.Marshal(responseJSON)
		h.logger.Debug("Claude 响应体: %s", string(responseJSONBytes))
	}

	// 修改响应中的模型ID (Claude结构)
	if responseJSON["model"] != nil {
		responseJSON["model"] = customModelID
	}

	h.writeJSON(w, responseJSON)
}

// writeJSON 写入JSON响应
func (h *Handler) writeJSON(w http.ResponseWriter, data interface{}, statusCode ...int) {
	w.Header().Set("Content-Type", "application/json")
	code := http.StatusOK
	if len(statusCode) > 0 {
		code = statusCode[0]
	}
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

// writeError 写入错误响应
func (h *Handler) writeError(w http.ResponseWriter, message string, statusCode int) {
	h.writeJSON(w, map[string]string{"error": message}, statusCode)
}
