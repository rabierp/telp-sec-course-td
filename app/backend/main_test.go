package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type mockOrderRepo struct {
	mu     sync.Mutex
	orders []Order
	pingOK bool
}

func (m *mockOrderRepo) SaveOrder(_ context.Context, o Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders = append(m.orders, o)
	return nil
}

func (m *mockOrderRepo) ListOrders(_ context.Context, _ int) ([]Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Order, len(m.orders))
	copy(out, m.orders)
	return out, nil
}

func (m *mockOrderRepo) Ping(_ context.Context) error {
	if !m.pingOK {
		return errors.New("firestore unreachable")
	}
	return nil
}

type mockStorageStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	pingOK  bool
}

func (m *mockStorageStore) WriteReceipt(_ context.Context, bucket, objectName string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objects == nil {
		m.objects = make(map[string][]byte)
	}
	m.objects[bucket+"/"+objectName] = append([]byte(nil), data...)
	return nil
}

func (m *mockStorageStore) ReadReceipt(_ context.Context, bucket, objectName string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	val, ok := m.objects[bucket+"/"+objectName]
	if !ok {
		return nil, errors.New("object not found")
	}
	return val, nil
}

func (m *mockStorageStore) Ping(_ context.Context, _ string) error {
	if !m.pingOK {
		return errors.New("gcs unreachable")
	}
	return nil
}

func TestCreateOrder_SignsAndPersists(t *testing.T) {
	repo := &mockOrderRepo{pingOK: true}
	store := &mockStorageStore{pingOK: true}
	secret := "SUPER_SECRET_KEY_2026"
	kmsKey := "projects/telp-etudiant-test-sec/locations/europe-west1/keyRings/kr-central/cryptoKeys/key-app-storage"

	srv := NewBackendServer(BackendConfig{
		ProjectID:            "telp-etudiant-test-back",
		ReceiptsBucket:       "telp-etudiant-test-back-receipts",
		ReceiptSigningSecret: secret,
		KMSKeyName:           kmsKey,
	}, repo, store)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	payload := `{"order_id":"ord-500","customer_name":"Etudiant Test","customer_email":"etudiant.test@gcp.telecom-paris.fr","amount_eur":125.50,"items":["hardware-token"]}`
	resp, err := http.Post(ts.URL+"/api/orders", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}

	var created OrderReceipt
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.OrderID != "ord-500" {
		t.Fatalf("expected order_id ord-500, got %q", created.OrderID)
	}
	if !created.SignatureValid {
		t.Fatalf("expected signature_valid=true")
	}
	if created.KMSKeyName != kmsKey {
		t.Fatalf("expected kms_key_name %q, got %q", kmsKey, created.KMSKeyName)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%.2f", "ord-500", "etudiant.test@gcp.telecom-paris.fr", 125.50)
	expectedHMAC := hex.EncodeToString(mac.Sum(nil))
	if created.ReceiptHMACSHA256 != expectedHMAC {
		t.Fatalf("expected HMAC %q, got %q", expectedHMAC, created.ReceiptHMACSHA256)
	}

	if len(repo.orders) != 1 {
		t.Fatalf("expected 1 order in Firestore repo, got %d", len(repo.orders))
	}
	if _, ok := store.objects["telp-etudiant-test-back-receipts/receipts/ord-500.json"]; !ok {
		t.Fatalf("expected receipt object stored in GCS mock")
	}
}

func TestGetReceipt_ReturnsStoredObject(t *testing.T) {
	repo := &mockOrderRepo{pingOK: true}
	store := &mockStorageStore{pingOK: true}
	secret := "TEST_HMAC_SECRET"

	srv := NewBackendServer(BackendConfig{
		ProjectID:            "telp-etudiant-test-back",
		ReceiptsBucket:       "telp-etudiant-test-back-receipts",
		ReceiptSigningSecret: secret,
		KMSKeyName:           "projects/sec/locations/europe-west1/keyRings/kr-central/cryptoKeys/key-app-storage",
	}, repo, store)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Create order first
	_, err := http.Post(ts.URL+"/api/orders", "application/json", strings.NewReader(`{"order_id":"ord-900","customer_name":"Test","customer_email":"etudiant.test@gcp.telecom-paris.fr","amount_eur":80.00}`))
	if err != nil {
		t.Fatal(err)
	}

	// Fetch receipt
	resp, err := http.Get(ts.URL + "/api/receipts/ord-900")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	var receipt OrderReceipt
	if err := json.NewDecoder(resp.Body).Decode(&receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.OrderID != "ord-900" || !receipt.SignatureValid {
		t.Fatalf("unexpected receipt response: %+v", receipt)
	}

	// Also verify /healthz
	hResp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer hResp.Body.Close()
	if hResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /healthz, got %d", hResp.StatusCode)
	}
}
