// Package storage defines the application's object storage naming boundary.
package storage

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

type Source string
type Kind string

const (
	Upload    Source = "uploads"
	Generated Source = "generated"
	Image     Kind   = "images"
	Video     Kind   = "videos"
	Audio     Kind   = "audios"
)

// Scope uses the canvas and node IDs, not user-controlled names or filenames.
// These IDs organize files; they are not an authorization mechanism.
type Scope struct {
	CanvasID string `json:"canvasId"`
	NodeID   string `json:"nodeId"`
}

func ValidateScope(scope Scope) error {
	if !uuid.MatchString(scope.CanvasID) || !uuid.MatchString(scope.NodeID) {
		return fmt.Errorf("画布和节点 ID 必须为小写 UUID")
	}
	return nil
}

type KeyBuilder struct {
	root string
}

var pathSegment = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func NewKeyBuilder(prefix, environment string) (*KeyBuilder, error) {
	if !pathSegment.MatchString(environment) {
		return nil, fmt.Errorf("存储环境只能包含小写字母、数字、下划线和连字符，长度为 1～63")
	}
	if len(prefix) == 0 || len(prefix) > 160 {
		return nil, fmt.Errorf("存储目录前缀长度必须为 1～160")
	}
	for _, segment := range strings.Split(prefix, "/") {
		if !pathSegment.MatchString(segment) {
			return nil, fmt.Errorf("存储目录前缀包含无效路径段")
		}
	}
	return &KeyBuilder{root: prefix + "/" + environment}, nil
}

// NewKey always allocates a new object. Regeneration never overwrites old files.
// The caller must derive extension from validated media content, not its name.
func (b *KeyBuilder) NewKey(scope Scope, source Source, kind Kind, extension string) (string, error) {
	if err := ValidateScope(scope); err != nil {
		return "", err
	}
	if source != Upload && source != Generated {
		return "", fmt.Errorf("无效的素材来源")
	}
	var extensions string
	switch kind {
	case Image:
		extensions = ".jpg .png .webp .gif .avif .bmp"
	case Video:
		extensions = ".mp4 .webm .mov .m4v .ogv"
	case Audio:
		extensions = ".mp3 .wav"
	default:
		return "", fmt.Errorf("只支持图片、视频和音频资源")
	}
	valid := false
	for _, allowed := range strings.Fields(extensions) {
		if extension == allowed {
			valid = true
			break
		}
	}
	if !valid {
		return "", fmt.Errorf("文件扩展名与资源类型不符")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("无法分配资源 ID")
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	assetID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	return fmt.Sprintf("%s/canvases/%s/%s/%s/%s/%s%s", b.root, scope.CanvasID, source, kind, scope.NodeID, assetID, extension), nil
}
