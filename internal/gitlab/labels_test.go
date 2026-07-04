package gitlab

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetMRLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/7/merge_requests/3" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"iid":3,"title":"x","labels":["releaseguard:false-positive","bug"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	labels, err := c.GetMRLabels(7, 3)
	if err != nil {
		t.Fatalf("GetMRLabels: %v", err)
	}
	if len(labels) != 2 || labels[0] != "releaseguard:false-positive" {
		t.Fatalf("labels=%v", labels)
	}
}

func TestListMergedMRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/7/merge_requests" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("state") != "merged" {
			t.Errorf("state param missing: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		// fewer than per_page → single page
		w.Write([]byte(`[{"iid":10,"title":"a","labels":[]},{"iid":11,"title":"b","labels":["releaseguard:false-positive"]}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	mrs, err := c.ListMergedMRs(7, "2026-01-01T00:00:00Z", 20, 3)
	if err != nil {
		t.Fatalf("ListMergedMRs: %v", err)
	}
	if len(mrs) != 2 || mrs[1].IID != 11 || len(mrs[1].Labels) != 1 {
		t.Fatalf("mrs=%+v", mrs)
	}
}

func TestGetMRNotes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/7/merge_requests/3/notes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"body":"## 🔴 ReleaseGuard recommendation: HOLD"},{"body":"unrelated"}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	notes, err := c.GetMRNotes(7, 3)
	if err != nil {
		t.Fatalf("GetMRNotes: %v", err)
	}
	if len(notes) != 2 || notes[0] != "## 🔴 ReleaseGuard recommendation: HOLD" {
		t.Fatalf("notes=%v", notes)
	}
}
