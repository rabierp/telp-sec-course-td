package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Order struct {
	OrderID       string   `json:"order_id"`
	CustomerName  string   `json:"customer_name"`
	CustomerEmail string   `json:"customer_email"`
	IBAN          string   `json:"iban,omitempty"`
	CreditCard    string   `json:"credit_card,omitempty"`
	AmountEUR     float64  `json:"amount_eur"`
	Items         []string `json:"items,omitempty"`
	CreatedAtUTC  string   `json:"created_at_utc"`
}

type OrderReceipt struct {
	OrderID           string   `json:"order_id"`
	CustomerName      string   `json:"customer_name"`
	CustomerEmail     string   `json:"customer_email"`
	AmountEUR         float64  `json:"amount_eur"`
	Items             []string `json:"items,omitempty"`
	ReceiptHMACSHA256 string   `json:"receipt_hmac_sha256"`
	SignatureValid    bool     `json:"signature_valid"`
	KMSKeyName        string   `json:"kms_key_name,omitempty"`
	CreatedAtUTC      string   `json:"created_at_utc"`
}

type OrderRepository interface {
	SaveOrder(ctx context.Context, order Order) error
	ListOrders(ctx context.Context, limit int) ([]Order, error)
	Ping(ctx context.Context) error
}

type StorageStore interface {
	WriteReceipt(ctx context.Context, bucket, objectName string, data []byte) error
	ReadReceipt(ctx context.Context, bucket, objectName string) ([]byte, error)
	Ping(ctx context.Context, bucket string) error
}

type BackendConfig struct {
	ProjectID            string
	ReceiptsBucket       string
	FirestoreDB          string
	FirestoreCollection  string
	ReceiptSigningSecret string
	KMSKeyName           string
}

type BackendServer struct {
	cfg   BackendConfig
	repo  OrderRepository
	store StorageStore
}

func NewBackendServer(cfg BackendConfig, repo OrderRepository, store StorageStore) *BackendServer {
	if cfg.FirestoreDB == "" {
		cfg.FirestoreDB = "(default)"
	}
	if cfg.FirestoreCollection == "" {
		cfg.FirestoreCollection = "orders"
	}
	if cfg.ReceiptSigningSecret == "" {
		cfg.ReceiptSigningSecret = "INSECURE_DEFAULT_SECRET_2026"
	}
	return &BackendServer{
		cfg:   cfg,
		repo:  repo,
		store: store,
	}
}

func (s *BackendServer) computeHMAC(orderID, email string, amount float64) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.ReceiptSigningSecret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%.2f", orderID, email, amount)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *BackendServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealthz)
	mux.HandleFunc("/api/health", s.handleHealthz)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/orders", s.handleOrders)
	mux.HandleFunc("/api/receipts/", s.handleReceiptByID)
	return mux
}

func (s *BackendServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if err := s.repo.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":    "error",
			"firestore": err.Error(),
		})
		return
	}
	if err := s.store.Ping(ctx, s.cfg.ReceiptsBucket); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":    "error",
			"firestore": "connected",
			"gcs":       err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"firestore": "connected",
		"gcs":       "connected",
	})
}

func (s *BackendServer) handleOrders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		orders, err := s.repo.ListOrders(r.Context(), 20)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(orders)

	case http.MethodPost:
		var order Order
		if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON payload"})
			return
		}
		if order.OrderID == "" {
			order.OrderID = fmt.Sprintf("ord-%d", time.Now().UnixNano())
		}
		if order.CustomerEmail == "" {
			order.CustomerEmail = "customer@gcp.telecom-paris.fr"
		}
		order.CreatedAtUTC = time.Now().UTC().Format(time.RFC3339)

		if err := s.repo.SaveOrder(r.Context(), order); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "firestore save failed: " + err.Error()})
			return
		}

		sig := s.computeHMAC(order.OrderID, order.CustomerEmail, order.AmountEUR)
		receipt := OrderReceipt{
			OrderID:           order.OrderID,
			CustomerName:      order.CustomerName,
			CustomerEmail:     order.CustomerEmail,
			AmountEUR:         order.AmountEUR,
			Items:             order.Items,
			ReceiptHMACSHA256: sig,
			SignatureValid:    true,
			KMSKeyName:        s.cfg.KMSKeyName,
			CreatedAtUTC:      order.CreatedAtUTC,
		}
		receiptBytes, err := json.Marshal(receipt)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := s.store.WriteReceipt(r.Context(), s.cfg.ReceiptsBucket, "receipts/"+order.OrderID+".json", receiptBytes); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "gcs write failed: " + err.Error()})
			return
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(receiptBytes)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
	}
}

func (s *BackendServer) handleReceiptByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := strings.TrimPrefix(r.URL.Path, "/api/receipts/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing receipt id"})
		return
	}
	raw, err := s.store.ReadReceipt(r.Context(), s.cfg.ReceiptsBucket, "receipts/"+id+".json")
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	var receipt OrderReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	expectedSig := s.computeHMAC(receipt.OrderID, receipt.CustomerEmail, receipt.AmountEUR)
	receipt.SignatureValid = hmac.Equal([]byte(receipt.ReceiptHMACSHA256), []byte(expectedSig))
	if s.cfg.KMSKeyName != "" && receipt.KMSKeyName == "" {
		receipt.KMSKeyName = s.cfg.KMSKeyName
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(receipt)
}

// --- GCP REST API Clients (Metadata Server / ADC Token) ---

func getGCPAccessToken(ctx context.Context) (string, error) {
	if tok := os.Getenv("CLOUDSDK_AUTH_ACCESS_TOKEN"); tok != "" {
		return strings.TrimSpace(tok), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata token status %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.AccessToken, nil
}

type gcpFirestoreRepo struct {
	projectID  string
	databaseID string
	collection string
	client     *http.Client
}

func (r *gcpFirestoreRepo) Ping(ctx context.Context) error {
	tok, err := getGCPAccessToken(ctx)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("https://firestore.googleapis.com/v1/projects/%s/databases/%s", url.PathEscape(r.projectID), url.PathEscape(r.databaseID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("firestore ping HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (r *gcpFirestoreRepo) SaveOrder(ctx context.Context, order Order) error {
	tok, err := getGCPAccessToken(ctx)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("https://firestore.googleapis.com/v1/projects/%s/databases/%s/documents/%s?documentId=%s",
		url.PathEscape(r.projectID), url.PathEscape(r.databaseID), url.PathEscape(r.collection), url.QueryEscape(order.OrderID))

	doc := map[string]any{
		"fields": map[string]any{
			"order_id":       map[string]any{"stringValue": order.OrderID},
			"customer_name":  map[string]any{"stringValue": order.CustomerName},
			"customer_email": map[string]any{"stringValue": order.CustomerEmail},
			"amount_eur":     map[string]any{"doubleValue": order.AmountEUR},
			"created_at_utc": map[string]any{"stringValue": order.CreatedAtUTC},
		},
	}
	bodyBytes, _ := json.Marshal(doc)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("firestore create document HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (r *gcpFirestoreRepo) ListOrders(ctx context.Context, limit int) ([]Order, error) {
	tok, err := getGCPAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("https://firestore.googleapis.com/v1/projects/%s/databases/%s/documents/%s?pageSize=%d",
		url.PathEscape(r.projectID), url.PathEscape(r.databaseID), url.PathEscape(r.collection), limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return []Order{}, nil
	}
	var res struct {
		Documents []struct {
			Fields map[string]struct {
				StringValue string  `json:"stringValue"`
				DoubleValue float64 `json:"doubleValue"`
			} `json:"fields"`
		} `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return []Order{}, nil
	}
	out := make([]Order, 0, len(res.Documents))
	for _, d := range res.Documents {
		out = append(out, Order{
			OrderID:       d.Fields["order_id"].StringValue,
			CustomerName:  d.Fields["customer_name"].StringValue,
			CustomerEmail: d.Fields["customer_email"].StringValue,
			AmountEUR:     d.Fields["amount_eur"].DoubleValue,
			CreatedAtUTC:  d.Fields["created_at_utc"].StringValue,
		})
	}
	return out, nil
}

type gcpStorageStore struct {
	client *http.Client
}

func (g *gcpStorageStore) Ping(ctx context.Context, bucket string) error {
	tok, err := getGCPAccessToken(ctx)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("https://storage.googleapis.com/storage/v1/b/%s", url.PathEscape(bucket))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gcs ping HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (g *gcpStorageStore) WriteReceipt(ctx context.Context, bucket, objectName string, data []byte) error {
	tok, err := getGCPAccessToken(ctx)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("https://storage.googleapis.com/upload/storage/v1/b/%s/o?uploadType=media&name=%s",
		url.PathEscape(bucket), url.QueryEscape(objectName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gcs upload HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (g *gcpStorageStore) ReadReceipt(ctx context.Context, bucket, objectName string) ([]byte, error) {
	tok, err := getGCPAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("https://storage.googleapis.com/storage/v1/b/%s/o/%s?alt=media",
		url.PathEscape(bucket), url.PathEscape(objectName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gcs read HTTP %d: %s", resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("PROJECT_ID")
	}
	bucket := os.Getenv("RECEIPTS_BUCKET")
	if bucket == "" && projectID != "" {
		bucket = projectID + "-receipts"
	}
	db := os.Getenv("FIRESTORE_DB")
	if db == "" {
		db = "(default)"
	}
	col := os.Getenv("FIRESTORE_COLLECTION")
	if col == "" {
		col = "orders"
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	repo := &gcpFirestoreRepo{
		projectID:  projectID,
		databaseID: db,
		collection: col,
		client:     httpClient,
	}
	store := &gcpStorageStore{client: httpClient}

	srv := NewBackendServer(BackendConfig{
		ProjectID:            projectID,
		ReceiptsBucket:       bucket,
		FirestoreDB:          db,
		FirestoreCollection:  col,
		ReceiptSigningSecret: os.Getenv("RECEIPT_SIGNING_SECRET"),
		KMSKeyName:           os.Getenv("KMS_KEY_NAME"),
	}, repo, store)

	log.Printf("TelP Secure Shop backend listening on :%s (project=%s, bucket=%s)", port, projectID, bucket)
	if err := http.ListenAndServe(":"+port, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
