package bytebin

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golem-mc/spark/internal/sparkproto"
	"google.golang.org/protobuf/proto"
)

func TestPostProto(t *testing.T) {
	var got sparkproto.HeapData
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/post" || r.Header.Get("Content-Type") != HeapMediaType ||
			r.Header.Get("Content-Encoding") != "gzip" || r.Header.Get("User-Agent") != "spark-plugin/live" {
			t.Errorf("request %s %v", r.URL.Path, r.Header)
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(zr)
		if err := proto.Unmarshal(data, &got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Location", "abc123")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	msg := &sparkproto.HeapData{Entries: []*sparkproto.HeapEntry{{Order: 1, Instances: 2, Size: 3, Type: "x"}}}
	key, err := New(srv.URL, "spark-plugin").PostProto(context.Background(), msg, HeapMediaType, "live")
	if err != nil || key != "abc123" {
		t.Fatalf("key=%q err=%v", key, err)
	}
	if !proto.Equal(&got, msg) {
		t.Errorf("uploaded %v", &got)
	}
}
