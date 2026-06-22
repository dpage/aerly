package handlers

import (
	"net/http"
	"strconv"
	"testing"
)

// TestG3AutoShareBadID covers the pathID-parse 400 on both the PUT and DELETE
// routes (a non-numeric {userId}).
func TestG3AutoShareBadID(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "g3asbadid", false)
	if w := e.req(t, "PUT", "/api/me/auto-shares/notanumber",
		map[string]any{"role": "viewer"}, me); w.Code != http.StatusBadRequest {
		t.Errorf("put bad id = %d, want 400", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/me/auto-shares/notanumber", nil, me); w.Code != http.StatusBadRequest {
		t.Errorf("delete bad id = %d, want 400", w.Code)
	}
}

// TestG3AutoShareBadBody covers the decode-failure 400 on PUT.
func TestG3AutoShareBadBody(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "g3asbadbody", false)
	friend := e.user(t, "g3asbf", false)
	w := e.req(t, "PUT", "/api/me/auto-shares/"+strconv.FormatInt(friend, 10), "not-json", me)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400", w.Code)
	}
}

// TestG3AutoShareListStoreErr drives the listMyAutoShares 500 path by dropping
// the backing table.
func TestG3AutoShareListStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "g3aslist", false)
	g1dropTable(t, e, "user_auto_shares")
	if w := e.req(t, "GET", "/api/me/auto-shares", nil, me); w.Code != http.StatusInternalServerError {
		t.Errorf("list store err = %d, want 500", w.Code)
	}
}

// TestG3AutoShareSetStoreErr drives the setMyAutoShare SetAutoShare 500 path:
// the target user exists (so we pass the 404 guard) but the insert table is
// gone.
func TestG3AutoShareSetStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "g3assetme", false)
	friend := e.user(t, "g3assetfr", false)
	g1dropTable(t, e, "user_auto_shares")
	w := e.req(t, "PUT", "/api/me/auto-shares/"+strconv.FormatInt(friend, 10),
		map[string]any{"role": "viewer"}, me)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("set store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG3AutoShareDeleteStoreErr drives the deleteMyAutoShare RemoveAutoShare
// 500 path.
func TestG3AutoShareDeleteStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "g3asdel", false)
	friend := e.user(t, "g3asdelfr", false)
	g1dropTable(t, e, "user_auto_shares")
	w := e.req(t, "DELETE", "/api/me/auto-shares/"+strconv.FormatInt(friend, 10), nil, me)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("delete store err = %d, want 500", w.Code)
	}
}
