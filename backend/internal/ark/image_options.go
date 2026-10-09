package ark

// ImageOptions describes one output. Batch counts belong to the canvas: each
// image has its own task and billing lifecycle, including for Pro.
type ImageOptions struct {
	Resolution string `json:"resolution"`
	Ratio      string `json:"ratio"`
}

// Sizes follow the provider's model-specific presets, verified 2026-10-09:
// https://docs.volcengine.com/docs/ark/image-generation-api?lang=zh
var liteImageSizes = map[string]map[string]string{
	"2K": {"1:1": "2048x2048", "4:3": "2304x1728", "3:4": "1728x2304", "16:9": "2848x1600", "9:16": "1600x2848", "3:2": "2496x1664", "2:3": "1664x2496", "21:9": "3136x1344"},
	"3K": {"1:1": "3072x3072", "4:3": "3456x2592", "3:4": "2592x3456", "16:9": "4096x2304", "9:16": "2304x4096", "3:2": "3744x2496", "2:3": "2496x3744", "21:9": "4704x2016"},
	"4K": {"1:1": "4096x4096", "4:3": "4704x3520", "3:4": "3520x4704", "16:9": "5504x3040", "9:16": "3040x5504", "3:2": "4992x3328", "2:3": "3328x4992", "21:9": "6240x2656"},
}
var proImageSizes = map[string]map[string]string{
	"1K":   {"1:1": "1024x1024", "4:3": "1152x864", "3:4": "864x1152", "16:9": "1424x800", "9:16": "800x1424", "3:2": "1248x832", "2:3": "832x1248", "21:9": "1568x672"},
	"1.5K": {"1:1": "1536x1536", "4:3": "1792x1344", "3:4": "1344x1792", "16:9": "2048x1152", "9:16": "1152x2048", "3:2": "1872x1248", "2:3": "1248x1872", "21:9": "2352x1008"},
	"2K":   {"1:1": "2048x2048", "4:3": "2368x1776", "3:4": "1776x2368", "16:9": "2816x1584", "9:16": "1584x2816", "3:2": "2496x1664", "2:3": "1664x2496", "21:9": "3136x1344"},
}

func ResolveImageOptions(model string, options *ImageOptions) (ImageOptions, error) {
	model, err := ResolveImageModel(model)
	if err != nil {
		return ImageOptions{}, err
	}
	value := ImageOptions{Resolution: "2K", Ratio: "adaptive"}
	if options != nil {
		if options.Resolution != "" {
			value.Resolution = options.Resolution
		}
		if options.Ratio != "" {
			value.Ratio = options.Ratio
		}
	}
	sizes := liteImageSizes
	if model == ProImageModel {
		sizes = proImageSizes
	}
	if sizes[value.Resolution] == nil {
		return ImageOptions{}, &APIError{400, "INVALID_IMAGE_RESOLUTION", "所选图片模型不支持此分辨率，请重新选择。"}
	}
	if value.Ratio != "adaptive" && sizes[value.Resolution][value.Ratio] == "" {
		return ImageOptions{}, &APIError{400, "INVALID_IMAGE_RATIO", "请选择支持的图片比例。"}
	}
	return value, nil
}

func ImageSize(model string, options *ImageOptions) (string, error) {
	model, err := ResolveImageModel(model)
	if err != nil {
		return "", err
	}
	value, err := ResolveImageOptions(model, options)
	if err != nil {
		return "", err
	}
	if value.Ratio == "adaptive" {
		return value.Resolution, nil
	}
	if model == ProImageModel {
		return proImageSizes[value.Resolution][value.Ratio], nil
	}
	return liteImageSizes[value.Resolution][value.Ratio], nil
}
