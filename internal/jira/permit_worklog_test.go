package jira

import (
	"encoding/json"
	"net/http"
	"testing"
)

// One entry, one person. A comment naming nobody credits whoever held the
// token; a comment naming two people cannot be read back without guessing.
func TestWorklogCommentMentionsExactlyOnePerson(t *testing.T) {
	add := func(comment any) error {
		body := logged()
		body["comment"] = comment
		return assertPermitted(http.MethodPost, worklog, worklogQuery, body, on)
	}
	mustAllow(t, add(adf(mention("account-a"))), "one mention")
	mustAllow(t, add(adf(text("note "), mention("account-a"), text(" more"))), "one mention with text")
	nested := map[string]any{"type": "doc", "version": 1, "content": []any{
		map[string]any{"type": "bulletList", "content": []any{
			map[string]any{"type": "listItem", "content": []any{adf(mention("account-a"))["content"].([]any)[0]}},
		}},
	}}
	mustAllow(t, add(nested), "one mention deeper in the tree")

	mustRefuse(t, add(adf(text("no one named"))), "zero mentions")
	mustRefuse(t, add(adf()), "empty paragraph")
	mustRefuse(t, add(adf(mention("account-a"), mention("account-b"))), "two mentions")
	mustRefuse(t, add(adf(mention("account-a"), mention("account-a"))), "the same person twice")
	mustRefuse(t, add(adf(mention(""))), "a mention of nobody")
	mustRefuse(t, add("Person A did this"), "a plain string")
	mustRefuse(t, add(map[string]any{"type": "paragraph", "content": []any{mention("account-a")}}), "not a document")
	mustRefuse(t, add(nil), "null")
}

// The query is exact: the two parameters, once each, and nothing else.
func TestWorklogQueryIsExact(t *testing.T) {
	for _, q := range []string{
		"",
		"notifyUsers=false",
		"adjustEstimate=leave",
		"notifyUsers=true&adjustEstimate=leave",
		"notifyUsers=false&adjustEstimate=auto",
		"notifyUsers=false&adjustEstimate=leave&expand=properties",
		"notifyUsers=false&notifyUsers=false&adjustEstimate=leave",
	} {
		mustRefuse(t, assertPermitted(http.MethodPost, worklog, q, logged(), on), "POST worklog?"+q)
		mustRefuse(t, assertPermitted(http.MethodPut, entry, q, logged(), on), "PUT worklog entry?"+q)
		mustRefuse(t, assertPermitted(http.MethodDelete, entry, q, nil, on), "DELETE worklog entry?"+q)
	}
	// Order is not meaning; url.Values encodes sorted.
	sorted := "adjustEstimate=leave&notifyUsers=false"
	mustAllow(t, assertPermitted(http.MethodPost, worklog, sorted, logged(), on), "the query in sorted order")
}

func TestWorklogFieldsAreChecked(t *testing.T) {
	with := func(k string, v any) map[string]any {
		body := logged()
		body[k] = v
		return body
	}
	without := func(k string) map[string]any {
		body := logged()
		delete(body, k)
		return body
	}
	add := func(body any) error { return assertPermitted(http.MethodPost, worklog, worklogQuery, body, on) }
	update := func(body any) error { return assertPermitted(http.MethodPut, entry, worklogQuery, body, on) }

	mustRefuse(t, add(with("timeSpentSeconds", 0)), "zero seconds")
	mustRefuse(t, add(with("timeSpentSeconds", -3600)), "negative seconds")
	mustRefuse(t, add(with("timeSpentSeconds", "3600")), "seconds as a string")
	mustRefuse(t, add(with("started", "")), "empty started")
	mustRefuse(t, add(with("started", 1758300000)), "started as a number")
	mustRefuse(t, add(with("visibility", map[string]any{"type": "group"})), "unknown key visibility")
	mustRefuse(t, add(with("author", map[string]any{"accountId": "account-a"})), "unknown key author")
	mustRefuse(t, add(with("timeSpent", "6h")), "timeSpent instead of seconds")
	for _, k := range []string{"started", "timeSpentSeconds", "comment"} {
		mustRefuse(t, add(without(k)), "add without "+k)
	}
	mustRefuse(t, add(nil), "add with no body")

	// An update may correct any one thing, but has to correct something,
	// and what it sends is checked the same way.
	mustAllow(t, update(map[string]any{"started": "2026-09-18T09:00:00.000+0000"}), "update started alone")
	mustAllow(t, update(map[string]any{"timeSpentSeconds": 1800}), "update seconds alone")
	mustAllow(t, update(map[string]any{"comment": adf(mention("account-b"))}), "update comment alone")
	mustAllow(t, update(logged()), "update all three")
	mustRefuse(t, update(map[string]any{}), "update nothing")
	mustRefuse(t, update(nil), "update with no body")
	mustRefuse(t, update(map[string]any{"timeSpentSeconds": 0}), "update to zero seconds")
	mustRefuse(t, update(map[string]any{"comment": adf(mention("account-a"), mention("account-b"))}), "update to two people")
	mustRefuse(t, update(map[string]any{"issueId": "10001"}), "update an unknown key")
}

// The path names the entry and there is nothing else to say.
func TestWorklogDeleteCarriesNoBody(t *testing.T) {
	mustAllow(t, assertPermitted(http.MethodDelete, entry, worklogQuery, nil, on), "delete")
	mustRefuse(t, assertPermitted(http.MethodDelete, entry, worklogQuery, map[string]any{}, on), "delete with an empty object")
	mustRefuse(t, assertPermitted(http.MethodDelete, entry, worklogQuery, logged(), on), "delete with an entry")
	var typedNil *struct{ Started string }
	mustRefuse(t, assertPermitted(http.MethodDelete, entry, worklogQuery, typedNil, on), "delete with a typed nil")
}

func TestCountMentions(t *testing.T) {
	two, _ := json.Marshal(adf(mention("account-a"), text(" and "), mention("account-b")))
	if got := countMentions(two); got != 2 {
		t.Errorf("countMentions = %d, want 2", got)
	}
	if countMentions([]byte(`not json`)) != 0 {
		t.Error("garbage is not a mention")
	}
}
