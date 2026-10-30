package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

// A pull request marked seen is kept with the commit the browser showed,
// marked again on a newer one, and withdrawn; a mark with no commit is
// refused, because nothing could ever bring that pull request back.
func TestSeenMarks(t *testing.T) {
	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(nil)
	srv.SeenRoutes(st)

	call := func(method, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, "/api/prs/seen", strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	seen := func() map[string]string {
		_, out := call("GET", "")
		got := map[string]string{}
		for _, raw := range out["seen"].([]any) {
			a := raw.(map[string]any)
			got[a["key"].(string)] = a["watermark"].(string)
		}
		return got
	}

	if code, _ := call("POST", `{"items":[{"key":"web-app#12","head":"aaa"},{"key":"api#3","head":"bbb"}]}`); code != http.StatusOK {
		t.Fatalf("marking seen: %d", code)
	}
	if got := seen(); got["web-app#12"] != "aaa" || got["api#3"] != "bbb" {
		t.Errorf("seen = %v", got)
	}
	call("POST", `{"items":[{"key":"web-app#12","head":"ccc"}]}`)
	if got := seen(); got["web-app#12"] != "ccc" || len(got) != 2 {
		t.Errorf("marking again should move the commit: %v", got)
	}
	if code, _ := call("POST", `{"items":[{"key":"web-app#12"}]}`); code != http.StatusBadRequest {
		t.Errorf("a mark with no commit should be refused, got %d", code)
	}
	if code, _ := call("DELETE", `{"keys":["web-app#12"]}`); code != http.StatusOK {
		t.Fatalf("withdrawing: %d", code)
	}
	if got := seen(); len(got) != 1 || got["api#3"] != "bbb" {
		t.Errorf("after withdrawing: %v", got)
	}
}
