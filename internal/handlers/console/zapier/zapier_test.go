// SPDX-License-Identifier: Apache-2.0

package zapier

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubscribeAndUnsubscribe(t *testing.T) {
	store := NewMemStore()
	h := NewHandler(store)

	body, _ := json.Marshal(map[string]string{
		"target_url": "https://hooks.zapier.com/abc",
		"event":      "feedback.created",
	})
	req := httptest.NewRequest(http.MethodPost, "/zapier/subscribe", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant1")
	rec := httptest.NewRecorder()
	h.Subscribe(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe status = %d, want 201", rec.Code)
	}

	var sub Subscription
	if err := json.NewDecoder(rec.Body).Decode(&sub); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode: %v", err)
	}
	if sub.TargetURL != "https://hooks.zapier.com/abc" {
		t.Errorf("target_url = %q", sub.TargetURL)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/zapier/subscribe/"+sub.ID, nil)
	delReq.Header.Set("X-Tenant-ID", "tenant1")
	delReq.SetPathValue("id", sub.ID)
	delRec := httptest.NewRecorder()
	h.Unsubscribe(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("unsubscribe status = %d, want 200", delRec.Code)
	}
}

func TestSubscribe_MissingTenant(t *testing.T) {
	store := NewMemStore()
	h := NewHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/zapier/subscribe", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	h.Subscribe(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
