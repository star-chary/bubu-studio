package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type copyTestObjects struct {
	data     map[string][]byte
	metadata map[string]string
	copies   int
	fail     bool
}

func (o *copyTestObjects) SignGet(context.Context, string) (string, error) { return "", nil }
func (o *copyTestObjects) Put(_ context.Context, key, _ string, _ int64, r io.Reader, _ map[string]string) error {
	o.data[key], _ = io.ReadAll(r)
	return nil
}
func (o *copyTestObjects) Get(_ context.Context, key, _ string) (*Content, error) {
	return &Content{Body: io.NopCloser(bytes.NewReader(o.data[key])), Status: http.StatusOK}, nil
}
func (o *copyTestObjects) Copy(_ context.Context, source, target, _ string, metadata map[string]string) error {
	o.copies++
	if o.fail {
		return errors.New("private provider failure")
	}
	value, ok := o.data[source]
	if !ok {
		return errors.New("missing source")
	}
	o.data[target] = bytes.Clone(value)
	o.metadata = metadata
	return nil
}

func TestCanvasCopyUsesPrivateIndependentStableObject(t *testing.T) {
	keys, _ := NewKeyBuilder("frame-space", "dev")
	sourceKey, _ := keys.NewKey(testScope, Upload, Image, ".png")
	objects := &copyTestObjects{data: map[string][]byte{sourceKey: []byte("image contents")}}
	store := &Store{keys: keys, objects: objects}
	source := Asset{Key: sourceKey, Scope: testScope, Kind: Image, Source: Upload, ContentType: "image/png", Bytes: 14}
	target := Scope{CanvasID: testScope.CanvasID, NodeID: "30303030-3030-4030-8030-303030303030"}
	first, err := store.CopyCanvasAsset(context.Background(), source, target)
	if err != nil {
		t.Fatal(err)
	}
	if first.Scope != target || first.Key == source.Key || !strings.Contains(first.Key, "/"+target.NodeID+"/") || !bytes.Equal(objects.data[first.Key], objects.data[sourceKey]) {
		t.Fatalf("incorrect copied asset: %+v", first)
	}
	if objects.metadata["node-id"] != target.NodeID {
		t.Fatal("object metadata must belong to the new node")
	}
	second, err := store.CopyCanvasAsset(context.Background(), source, target)
	if err != nil || second.Key != first.Key || len(objects.data) != 2 {
		t.Fatal("retry must reuse the same copied object")
	}
	delete(objects.data, sourceKey)
	if string(objects.data[first.Key]) != "image contents" {
		t.Fatal("the copy must be independent of the source object")
	}
}

func TestCanvasCopyRejectsInvalidOwnershipAndHidesProviderFailure(t *testing.T) {
	keys, _ := NewKeyBuilder("frame-space", "dev")
	key, _ := keys.NewKey(testScope, Upload, Image, ".png")
	objects := &copyTestObjects{data: map[string][]byte{key: []byte("x")}}
	store := &Store{keys: keys, objects: objects}
	source := Asset{Key: key, Scope: testScope, Kind: Image, Source: Upload, ContentType: "image/png", Bytes: 1}
	target := Scope{CanvasID: testScope.CanvasID, NodeID: "30303030-3030-4030-8030-303030303030"}
	for _, bad := range []Asset{
		{Key: key, Scope: target, Kind: Image, Source: Upload, ContentType: "image/png", Bytes: 1},
		{Key: key, Scope: testScope, Kind: Video, Source: Upload, ContentType: "video/mp4", Bytes: 1},
	} {
		if _, err := store.CopyCanvasAsset(context.Background(), bad, target); err == nil {
			t.Fatal("forged source accepted")
		}
	}
	other := target
	other.CanvasID = "40404040-4040-4040-8040-404040404040"
	if _, err := store.CopyCanvasAsset(context.Background(), source, other); err == nil {
		t.Fatal("cross-canvas copy accepted")
	}
	if objects.copies != 0 {
		t.Fatal("invalid copies reached object storage")
	}
	objects.fail = true
	if _, err := store.CopyCanvasAsset(context.Background(), source, target); err == nil || strings.Contains(err.Error(), "private provider") {
		t.Fatal("provider failure must be sanitized")
	}
}

func TestOSSCopySendsPrivateDestinationMetadata(t *testing.T) {
	objects := fakeOSSClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPut || r.Header.Get("X-Oss-Metadata-Directive") != "REPLACE" ||
			r.Header.Get("X-Oss-Object-Acl") != "private" || r.Header.Get("X-Oss-Meta-Node-Id") != "new-node" ||
			!strings.Contains(r.Header.Get("X-Oss-Copy-Source"), "source.png") {
			t.Error("copy request did not retain private destination scope")
		}
		body := `<CopyObjectResult><LastModified>2026-11-12T00:00:00.000Z</LastModified><ETag>etag</ETag></CopyObjectResult>`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	if err := objects.Copy(context.Background(), "source.png", "target.png", "image/png", map[string]string{"node-id": "new-node"}); err != nil {
		t.Fatal(err)
	}
}
