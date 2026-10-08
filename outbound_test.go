package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleOutboundPush_AcceptedStatus(t *testing.T) {
	originalAsyncPush := asyncOutboundPush
	asyncOutboundPush = func(req OutboundPushRequest, storage StorageProvider) {
		// No-op: We are only testing the HTTP handler layer here
	}
	defer func() { asyncOutboundPush = originalAsyncPush }()

	storage := &MockStorageProvider{}
	handler := handleOutboundPush(storage)

	reqPayload := OutboundPushRequest{
		RemoteHost: "127.0.0.1:2222",
		Username:   "partner",
		Password:   "secret",
		SourceKey:  "/out/invoice.csv",
		TargetPath: "/inbound/invoice.csv",
	}

	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest("POST", "/api/v1/sftp/push", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("Expected status code 202 Accepted, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	if resp["status"] != "queued" {
		t.Errorf("Expected status queued, got %s", resp["status"])
	}
	if resp["target"] != "/inbound/invoice.csv" {
		t.Errorf("Expected target /inbound/invoice.csv, got %s", resp["target"])
	}
}

func TestHandleOutboundPush_MethodNotAllowed(t *testing.T) {
	handler := handleOutboundPush(&MockStorageProvider{})

	req := httptest.NewRequest("GET", "/api/v1/sftp/push", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 Method Not Allowed, got %d", rec.Code)
	}
}
