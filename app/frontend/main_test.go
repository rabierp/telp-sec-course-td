package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckoutProxy_WithoutOIDC(t *testing.T) {
	var receivedAuthHeader string
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/orders" {
			t.Errorf("unexpected backend request: %s %s", r.Method, r.URL.Path)
		}
		receivedAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"order_id":"ord-101","receipt_hmac_sha256":"abc123","signature_valid":true}`))
	}))
	defer backendSrv.Close()

	srv := NewFrontendServer(FrontendConfig{
		ProjectID:   "telp-etudiant-test-front",
		BackendURL:  backendSrv.URL,
		UseOIDCAuth: false,
	})
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Case 1: Caller provides Authorization header -> forwarded when USE_OIDC_AUTH=false
	req1, err := http.NewRequest(http.MethodPost, ts.URL+"/checkout", strings.NewReader(`{"customer_name":"Alice","customer_email":"alice@gcp.telecom-paris.fr","amount_eur":42.50}`))
	if err != nil {
		t.Fatal(err)
	}
	req1.Header.Set("Authorization", "Bearer caller-student-token")
	req1.Header.Set("Content-Type", "application/json")

	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp1.StatusCode)
	}
	if receivedAuthHeader != "Bearer caller-student-token" {
		t.Fatalf("expected forwarded Authorization header 'Bearer caller-student-token', got %q", receivedAuthHeader)
	}

	// Case 2: Caller omits Authorization header -> omitted when USE_OIDC_AUTH=false
	req2, err := http.NewRequest(http.MethodPost, ts.URL+"/checkout", strings.NewReader(`{"customer_name":"Bob","customer_email":"bob@gcp.telecom-paris.fr","amount_eur":19.99}`))
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if receivedAuthHeader != "" {
		t.Fatalf("expected empty Authorization header when caller omits it, got %q", receivedAuthHeader)
	}
}

func TestCheckoutProxy_WithOIDC(t *testing.T) {
	var metadataAudience string
	var metadataFlavor string
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadataAudience = r.URL.Query().Get("audience")
		metadataFlavor = r.Header.Get("Metadata-Flavor")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock-sa-frontend-oidc-jwt"))
	}))
	defer metaSrv.Close()

	var backendAuth string
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"order_id":"ord-202","receipt_hmac_sha256":"def456","signature_valid":true}`))
	}))
	defer backendSrv.Close()

	srv := NewFrontendServer(FrontendConfig{
		ProjectID:           "telp-etudiant-test-front",
		BackendURL:          backendSrv.URL,
		UseOIDCAuth:         true,
		MetadataIdentityURL: metaSrv.URL,
	})
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/checkout", strings.NewReader(`{"customer_name":"Test","customer_email":"etudiant.test@gcp.telecom-paris.fr","amount_eur":99.00}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer caller-token-should-be-replaced")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}
	if metadataFlavor != "Google" {
		t.Fatalf("expected Metadata-Flavor: Google, got %q", metadataFlavor)
	}
	if metadataAudience != backendSrv.URL {
		t.Fatalf("expected metadata audience %q, got %q", backendSrv.URL, metadataAudience)
	}
	if backendAuth != "Bearer mock-sa-frontend-oidc-jwt" {
		t.Fatalf("expected backend Authorization 'Bearer mock-sa-frontend-oidc-jwt', got %q", backendAuth)
	}
}

func TestAdminEndpoint_IAPHeader(t *testing.T) {
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"order_id":"ord-1","customer_email":"etudiant.test@gcp.telecom-paris.fr","amount_eur":50}]`))
	}))
	defer backendSrv.Close()

	srv := NewFrontendServer(FrontendConfig{
		ProjectID:  "telp-etudiant-test-front",
		BackendURL: backendSrv.URL,
	})
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Goog-Authenticated-User-Email", "accounts.google.com:etudiant.test@gcp.telecom-paris.fr")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	if !strings.Contains(body, "etudiant.test@gcp.telecom-paris.fr") {
		t.Fatalf("expected admin response to include authenticated user email, got: %s", body)
	}
}

func TestReceiptsAndHealthzEndpoints(t *testing.T) {
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/receipts/ord-777" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"order_id":"ord-777","signature_valid":true,"kms_key_name":"projects/sec/locations/europe-west1/keyRings/kr-central/cryptoKeys/key-app-storage"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backendSrv.Close()

	srv := NewFrontendServer(FrontendConfig{
		ProjectID:  "telp-etudiant-test-front",
		BackendURL: backendSrv.URL,
	})
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Healthz
	hResp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer hResp.Body.Close()
	if hResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /healthz, got %d", hResp.StatusCode)
	}
	var hData map[string]string
	if err := json.NewDecoder(hResp.Body).Decode(&hData); err != nil {
		t.Fatal(err)
	}
	if hData["status"] != "ok" || hData["service"] != "frontend" {
		t.Fatalf("unexpected healthz payload: %+v", hData)
	}

	// Receipts proxy
	rResp, err := http.Get(ts.URL + "/receipts/ord-777")
	if err != nil {
		t.Fatal(err)
	}
	defer rResp.Body.Close()
	if rResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /receipts/ord-777, got %d", rResp.StatusCode)
	}
}
