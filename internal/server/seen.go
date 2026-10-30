package server

import (
	"encoding/json"
	"net/http"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

// SeenRoutes registers the pull requests marked seen on the dashboard.
// Registered whenever there is a store to keep them in, so a browser
// that gets a 404 here offers no mark rather than one that fails.
//
// A mark is the backlog's acknowledgement under another kind: the key is
// repo#number and the watermark the head commit. The browser sends the
// commit it was showing, not the one the server read last, because that
// is the code the person looked at; a push since then brings the pull
// request back as changed. Nothing is written to GitHub.
func (s *Server) SeenRoutes(st store.Store) {
	s.mux.HandleFunc("GET /api/prs/seen", func(w http.ResponseWriter, r *http.Request) {
		acks, err := st.ListAcks(r.Context(), store.AckPR)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"seen": acks})
	})

	s.mux.HandleFunc("POST /api/prs/seen", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Items []struct {
				Key  string `json:"key"`
				Head string `json:"head"`
			} `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that request"})
			return
		}
		acks := make([]store.Ack, 0, len(body.Items))
		for _, it := range body.Items {
			// Without a commit there is nothing to tell a push by, and a
			// mark that never comes undone would hide changes for good.
			if it.Key == "" || it.Head == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "each pull request needs a key and a head commit"})
				return
			}
			acks = append(acks, store.Ack{Kind: store.AckPR, Key: it.Key, Watermark: it.Head})
		}
		if len(acks) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nothing named in items"})
			return
		}
		if err := st.PutAcks(r.Context(), acks); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"seen": len(acks)})
	})

	s.mux.HandleFunc("DELETE /api/prs/seen", func(w http.ResponseWriter, r *http.Request) {
		keys, ok := readKeys(w, r, "keys")
		if !ok {
			return
		}
		if err := st.DeleteAcks(r.Context(), store.AckPR, keys); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"withdrawn": len(keys)})
	})
}
