package streams

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

func TestV2StreamThumbnailIsOptionalAndMatchesRegeneratedContract(t *testing.T) {
	now := time.Now()
	live := Operation{ID: "11111111-1111-4111-8111-111111111111", Pool: "0x" + strings.Repeat("1", 40), Wallet: "0x" + strings.Repeat("2", 40), Title: "Poster stream", State: "LIVE", ProviderID: "provider_1", ProviderTime: &now, Revision: 3}
	viewers := int64(4)
	for _, tc := range []struct {
		name      string
		op        Operation
		sources   PlaybackSources
		wantKey   bool
		wantValue string
	}{
		{"live_with_thumbnail", live, PlaybackSources{URL: "https://livepeercdn.studio/hls/one/index.m3u8", Thumbnail: "https://livepeercdn.studio/hls/one/thumbnail.jpg"}, true, "https://livepeercdn.studio/hls/one/thumbnail.jpg"},
		{"live_without_thumbnail", live, PlaybackSources{URL: "https://livepeercdn.studio/hls/one/index.m3u8"}, false, ""},
		{"live_rejects_http_thumbnail", live, PlaybackSources{URL: "https://livepeercdn.studio/hls/one/index.m3u8", Thumbnail: "http://livepeercdn.studio/hls/one/thumbnail.jpg"}, false, ""},
		{"live_rejects_credentialed_thumbnail", live, PlaybackSources{URL: "https://livepeercdn.studio/hls/one/index.m3u8", Thumbnail: "https://user:secret@livepeercdn.studio/hls/one/thumbnail.jpg"}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := AdaptV2Stream(tc.op, tc.sources.URL, &viewers, "verified")
			if err != nil {
				t.Fatal(err)
			}
			view = withThumbnail(view, tc.sources)
			raw, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			if err = v2.ValidateJSON("Stream", raw); err != nil {
				t.Fatal(string(raw), err)
			}
			var object map[string]json.RawMessage
			if err = json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			if _, present := object["thumbnailUrl"]; present != tc.wantKey {
				t.Fatal(string(raw))
			}
			var decoded v2.Stream
			if err = json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.ThumbnailUrl.Present != tc.wantKey || decoded.ThumbnailUrl.Value != tc.wantValue || decoded.StreamId != live.ID || decoded.Status != "live" {
				t.Fatal(decoded)
			}
		})
	}
	ended := live
	ended.State = "ENDED"
	view, err := AdaptV2Stream(ended, "", &viewers, "verified")
	if err != nil {
		t.Fatal(err)
	}
	view = withThumbnail(view, PlaybackSources{Thumbnail: "https://livepeercdn.studio/hls/one/thumbnail.jpg"})
	if view.ThumbnailURL.Present {
		t.Fatal("ended stream carried a poster")
	}
}
