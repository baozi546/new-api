package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCPAAccountsRedactManagementResponse(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v8/management/credentials" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("wrong CPA request")
		}
		_, _ = w.Write([]byte(`{"files":[{"id":"one","provider":"codex","account_concurrency":2,"email":"private-marker","access_token":"private-marker","id_token":{"plan_type":"plus","account_id":"private-marker"}},{"id":"other","provider":"other"}]}`))
	}))
	defer s.Close()
	t.Setenv("CPA_MANAGEMENT_URL", s.URL)
	t.Setenv("CPA_MANAGEMENT_TOKEN", "fixture")
	rows, err := ListCPAAccounts(t.Context())
	if err != nil || len(rows) != 1 || rows[0].Pool != "plus" || *rows[0].Limit != 2 {
		t.Fatalf("unexpected list %v %v", rows, err)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "private-marker") {
		t.Fatal("private upstream metadata escaped")
	}
}

func TestCPAStockAccountHealth(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":[{"id":"limited","provider":"codex","quota_blocked":true,"unavailable":true,"status":"error"}]}`))
	}))
	defer s.Close()
	t.Setenv("CPA_MANAGEMENT_URL", s.URL)
	t.Setenv("CPA_MANAGEMENT_TOKEN", "fixture")
	accounts, err := ListCPAAccounts(t.Context())
	if err != nil || len(accounts) != 1 {
		t.Fatalf("unexpected list: %v %v", accounts, err)
	}
	a := accounts[0]
	if !a.QuotaBlocked || !a.Unavailable || a.Status != "error" || a.Limit != nil || a.Check.Result != "" {
		t.Fatalf("unexpected stock health: %#v", a)
	}
}
