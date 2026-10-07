package storage

import (
	"path"
	"strings"
	"testing"
)

var testScope = Scope{
	CanvasID: "f2476fbb-2af0-494f-9a38-f8f9bdf1e5ac",
	NodeID:   "56b46c49-fc0b-41cf-9db0-01493aa14d3f",
}

func TestObjectKeysSeparateScopeSourceAndKind(t *testing.T) {
	builder, err := NewKeyBuilder("frame-space", "dev")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, tc := range []struct {
		source    Source
		kind      Kind
		extension string
	}{
		{Upload, Image, ".png"}, {Upload, Video, ".mp4"}, {Generated, Image, ".webp"}, {Generated, Video, ".mp4"},
	} {
		for range 2 {
			key, err := builder.NewKey(testScope, tc.source, tc.kind, tc.extension)
			if err != nil {
				t.Fatal(err)
			}
			want := "frame-space/dev/canvases/" + testScope.CanvasID + "/" + string(tc.source) + "/" + string(tc.kind) + "/" + testScope.NodeID + "/"
			assetID := strings.TrimSuffix(path.Base(key), tc.extension)
			if !strings.HasPrefix(key, want) || !strings.HasSuffix(key, tc.extension) || !uuid.MatchString(assetID) || seen[key] {
				t.Fatalf("资源未正确分层或发生覆盖：%s", key)
			}
			seen[key] = true
		}
	}
	other := testScope
	other.CanvasID = "29238f21-53b7-414d-b36e-bc1a9a119d12"
	key, _ := builder.NewKey(other, Upload, Image, ".png")
	if strings.Contains(key, testScope.CanvasID) {
		t.Fatal("不同画布不应共享目录")
	}
	prod, _ := NewKeyBuilder("frame-space", "prod")
	key, _ = prod.NewKey(testScope, Upload, Image, ".png")
	if !strings.HasPrefix(key, "frame-space/prod/") {
		t.Fatal("环境未隔离")
	}
}

func TestObjectKeysRejectTraversalAndMismatchedTypes(t *testing.T) {
	for _, prefix := range []string{"", "../other", "frame-space/../other", "/absolute", "frame-space/", "frame-space//dev", `frame-space\dev`, "frame%2fother"} {
		if _, err := NewKeyBuilder(prefix, "dev"); err == nil {
			t.Fatalf("未拒绝目录：%s", prefix)
		}
	}
	if _, err := NewKeyBuilder("frame-space", "../prod"); err == nil {
		t.Fatal("环境不能改变路径层级")
	}
	builder, _ := NewKeyBuilder("frame-space", "dev")
	for _, tc := range []struct {
		scope     Scope
		source    Source
		kind      Kind
		extension string
	}{
		{Scope{CanvasID: "../other", NodeID: testScope.NodeID}, Upload, Image, ".png"},
		{Scope{CanvasID: testScope.CanvasID, NodeID: "../other"}, Upload, Image, ".png"},
		{testScope, "../uploads", Image, ".png"}, {testScope, Upload, "other", ".png"},
		{testScope, Upload, Image, ".mp4"}, {testScope, Upload, Video, ".png"},
		{testScope, Upload, Image, "/../../other.png"}, {testScope, Upload, Image, ".html"},
	} {
		if _, err := builder.NewKey(tc.scope, tc.source, tc.kind, tc.extension); err == nil {
			t.Fatal("应拒绝非法资源路径或类型")
		}
	}
}
