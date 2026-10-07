package ark

import "strings"

const DefaultImageModel = "doubao-seedream-5-0-260128"
const ProImageModel = "doubao-seedream-5-0-pro-260628"

func MaxReferenceImages(model string) int {
	if model == ProImageModel {
		return 10
	}
	return 14
}

// ResolveImageModel 保留旧请求省略 model 时的 Lite 行为，只接受当前开放的两个模型。
func ResolveImageModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultImageModel
	}
	if model != DefaultImageModel && model != ProImageModel {
		return "", &APIError{400, "INVALID_MODEL", "请选择支持的 Seedream 5.0 Lite 或 Pro 模型。"}
	}
	return model, nil
}
