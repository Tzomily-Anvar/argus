package jira

import (
	"errors"
	"net/http"
	"testing"
)

// The two Confluence writes: one page created under the parent, one page
// updated at its current version. Each carries a storage body with the
// markers, once each, in order, and nothing the list does not name.

const (
	pages = "/wiki/api/v2/pages"
	one   = pages + "/123"
)

func marked(inner string) string { return PageMarkerStart + inner + PageMarkerEnd }

func storage(value string) map[string]any {
	return map[string]any{"representation": "storage", "value": value}
}

func create(body map[string]any) map[string]any {
	return map[string]any{
		"spaceId": "100", "status": "current", "title": "Sprint 21 Report (live)",
		"parentId": "200", "body": body,
	}
}

func update(id string, version any, body map[string]any) map[string]any {
	return map[string]any{
		"id": id, "status": "current", "title": "Sprint 21 Report", "body": body, "version": version,
	}
}

func TestPageWritesAreTheTwoListed(t *testing.T) {
	mustAllow(t, assertPermitted(http.MethodPost, pages, "", create(storage(marked("<p>x</p>"))), on), "creating a page")
	mustAllow(t, assertPermitted(http.MethodPut, one, "", update("123", map[string]any{"number": 4}, storage(marked(""))), on), "updating a page")
	mustAllow(t, assertPermitted(http.MethodPut, one, "",
		update("123", map[string]any{"number": 4, "message": "Argus"}, storage(marked("x"))), on), "updating with a message")

	// Reads under /wiki/ are reads like any other.
	mustAllow(t, assertPermitted(http.MethodGet, pages, "space-id=100&title=x", nil, on), "searching by title")
	mustAllow(t, assertPermitted(http.MethodGet, one, "body-format=storage", nil, writePolicy{}), "reading a page with writes off")

	for _, req := range []request{
		{http.MethodPost, pages, "", create(storage(marked("x")))},
		{http.MethodPut, one, "", update("123", map[string]any{"number": 4}, storage(marked("x")))},
	} {
		var disabled *WritesDisabledError
		if err := assertPermitted(req.method, req.path, req.query, req.body, writePolicy{}); !errors.As(err, &disabled) {
			t.Errorf("%s %s with writes off: got %v, want *WritesDisabledError", req.method, req.path, err)
		}
	}
}

func TestPageBodiesAreExact(t *testing.T) {
	good := storage(marked("x"))
	withKey := func(m map[string]any, k string, v any) map[string]any {
		out := map[string]any{}
		for kk, vv := range m {
			out[kk] = vv
		}
		out[k] = v
		return out
	}
	without := func(m map[string]any, k string) map[string]any {
		out := withKey(m, k, nil)
		delete(out, k)
		return out
	}
	creates := map[string]any{
		"no body":              nil,
		"an array":             []any{},
		"a draft":              withKey(create(good), "status", "draft"),
		"an empty title":       withKey(create(good), "title", ""),
		"a numeric space id":   withKey(create(good), "spaceId", 100),
		"no parent":            without(create(good), "parentId"),
		"an empty parent":      withKey(create(good), "parentId", ""),
		"an extra key":         withKey(create(good), "subtype", "live"),
		"no markers":           create(storage("<p>x</p>")),
		"the start only":       create(storage(PageMarkerStart + "x")),
		"markers twice":        create(storage(marked("x") + marked("y"))),
		"markers out of order": create(storage(PageMarkerEnd + "x" + PageMarkerStart)),
		"wiki markup":          create(map[string]any{"representation": "wiki", "value": marked("x")}),
		"a body with more":     create(withKey(good, "atlas_doc_format", "x")),
		"a non-string value":   create(map[string]any{"representation": "storage", "value": 1}),
	}
	for name, body := range creates {
		mustRefuse(t, assertPermitted(http.MethodPost, pages, "", body, on), "creating with "+name)
	}

	updates := map[string]any{
		"no version":           without(update("123", 1, good), "version"),
		"version zero":         update("123", map[string]any{"number": 0}, good),
		"a fractional version": update("123", map[string]any{"number": 1.5}, good),
		"a string version":     update("123", map[string]any{"number": "4"}, good),
		"a bare number":        update("123", 4, good),
		"a version extra":      update("123", map[string]any{"number": 4, "minorEdit": true}, good),
		"a message not text":   update("123", map[string]any{"number": 4, "message": 1}, good),
		"another page's id":    update("124", map[string]any{"number": 4}, good),
		"a numeric id":         update("123", map[string]any{"number": 4}, good),
		"no id":                without(update("123", map[string]any{"number": 4}, good), "id"),
		"a space id":           withKey(update("123", map[string]any{"number": 4}, good), "spaceId", "100"),
		"no markers":           update("123", map[string]any{"number": 4}, storage("x")),
	}
	updates["a numeric id"] = withKey(update("123", map[string]any{"number": 4}, good), "id", 123)
	for name, body := range updates {
		mustRefuse(t, assertPermitted(http.MethodPut, one, "", body, on), "updating with "+name)
	}
}

// Everything else under /wiki/ stays refused: other verbs on the two
// paths, anything beneath a page, and the whole of the older REST API.
func TestOnlyPagesAreWritableUnderWiki(t *testing.T) {
	body := create(storage(marked("x")))
	for _, req := range []request{
		{http.MethodPost, one, "", body},
		{http.MethodPut, pages, "", body},
		{http.MethodDelete, one, "", nil},
		{http.MethodPost, one + "/attachments", "", body},
		{http.MethodPost, one + "/footer-comments", "", body},
		{http.MethodPost, one + "/labels", "", body},
		{http.MethodPut, one + "/properties/1", "", body},
		{http.MethodPost, "/wiki/api/v2/blogposts", "", body},
		{http.MethodPost, "/wiki/rest/api/content", "", body},
		{http.MethodPut, "/wiki/rest/api/content/123", "", body},
		{http.MethodPut, "/wiki/api/v2/pages/abc", "", body},
		{http.MethodPut, one + "/", "", body},
		{http.MethodPost, pages + "?private=true", "", body},
		{http.MethodPost, pages, "private=true", body},
		{http.MethodPut, one, "notifyUsers=false", update("123", map[string]any{"number": 4}, storage(marked("x")))},
	} {
		mustRefuse(t, assertPermitted(req.method, req.path, req.query, req.body, on), req.method+" "+req.path+"?"+req.query)
	}
}
