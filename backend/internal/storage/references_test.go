package storage

import (
	"bytes"
	"context"
	"errors"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"image"
	"image/png"
	"net/url"
	"strings"
	"testing"
)

type referenceObjects struct {
	memoryObjects
	gets, signs              int
	readFailure, signFailure bool
}

func (o *referenceObjects) Get(ctx context.Context, key, byteRange string) (*Content, error) {
	o.gets++
	if o.readFailure {
		return nil, errors.New("private-provider-details")
	}
	return o.memoryObjects.Get(ctx, key, byteRange)
}
func (o *referenceObjects) SignGet(ctx context.Context, key string) (string, error) {
	o.signs++
	if o.signFailure {
		return "", errors.New("private-provider-details")
	}
	return o.memoryObjects.SignGet(ctx, key)
}
func referenceFixture(t *testing.T) (*Store, *referenceObjects, Scope, string) {
	t.Helper()
	store, _ := testStore(t)
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 64, 48))); err != nil {
		t.Fatal(err)
	}
	objects := &referenceObjects{memoryObjects: memoryObjects{data: data.Bytes(), mime: "image/png"}}
	store.objects = objects
	target := Scope{CanvasID: testScope.CanvasID, NodeID: "55555555-5555-4555-8555-555555555555"}
	key, err := store.keys.NewKey(testScope, Upload, Image, ".png")
	if err != nil {
		t.Fatal(err)
	}
	return store, objects, target, key
}
func TestReferenceKeysCheckedBeforeCloudAccess(t *testing.T) {
	store, objects, target, key := referenceFixture(t)
	video, _ := store.keys.NewKey(testScope, Upload, Video, ".mp4")
	avif, _ := store.keys.NewKey(testScope, Upload, Image, ".avif")
	self, _ := store.keys.NewKey(target, Generated, Image, ".png")
	for _, keys := range [][]string{
		{key, key}, {"https://private.invalid/image.png"}, {video}, {avif}, {self},
		{strings.Replace(key, target.CanvasID, "66666666-6666-4666-8666-666666666666", 1)},
		{strings.Replace(key, "/dev/", "/prod/", 1)}, {key + "/../image.png"},
	} {
		if _, err := store.ReferenceURLs(context.Background(), target, keys); err == nil {
			t.Fatalf("accepted invalid reference %v", keys)
		}
	}
	if objects.gets != 0 || objects.signs != 0 {
		t.Fatal("invalid scope must fail before cloud access")
	}
}
func TestReferencesPreserveOrderAndCheckImageContent(t *testing.T) {
	store, objects, target, key := referenceFixture(t)
	second, _ := store.keys.NewKey(testScope, Generated, Image, ".png")
	urls, err := store.ReferenceURLs(context.Background(), target, []string{key, second})
	if err != nil || len(urls) != 2 || !strings.Contains(urls[0], key) || !strings.Contains(urls[1], second) || objects.signs != 2 {
		t.Fatalf("reference order lost: %v", err)
	}
	objects.signs = 0
	objects.data = samplePNG(t)
	if _, err := store.ReferenceURLs(context.Background(), target, []string{key}); err == nil || objects.signs != 0 {
		t.Fatal("invalid dimensions must not be signed")
	}
	objects.data = []byte("not an image")
	if _, err := store.ReferenceURLs(context.Background(), target, []string{key}); err == nil {
		t.Fatal("invalid image must fail")
	}
	for _, config := range []image.Config{{Width: 14, Height: 20}, {Width: 20, Height: 321}, {Width: 6001, Height: 6000}} {
		if validReferenceDimensions(config) {
			t.Fatal("accepted invalid image dimensions")
		}
	}
}
func TestReferenceErrorsDoNotLeakDetails(t *testing.T) {
	for _, phase := range []string{"read", "sign"} {
		store, objects, target, key := referenceFixture(t)
		objects.readFailure, objects.signFailure = phase == "read", phase == "sign"
		if _, err := store.ReferenceURLs(context.Background(), target, []string{key}); err == nil || strings.Contains(err.Error(), "private-provider") {
			t.Fatal("reference failure must be sanitized")
		}
	}
}
func TestOSSSignedReferenceExpiresAfterTenMinutes(t *testing.T) {
	client := oss.NewClient(oss.LoadDefaultConfig().WithRegion("cn-hangzhou").WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-access-id", "test-access-secret")))
	objects := &ossObjects{client: client, bucket: "test-bucket"}
	address, err := objects.SignGet(context.Background(), "frame-space/dev/test.png")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "test-bucket.oss-cn-hangzhou.aliyuncs.com" || parsed.Query().Get("x-oss-expires") != "600" || parsed.Query().Get("x-oss-signature") == "" {
		t.Fatal("signed reference URL must expire in 10 minutes")
	}
}
