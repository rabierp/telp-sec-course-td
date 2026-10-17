package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type FrontendConfig struct {
	ProjectID           string
	BackendURL          string
	AIAssistantURL      string
	UseOIDCAuth         bool
	MetadataIdentityURL string
}

type FrontendServer struct {
	cfg    FrontendConfig
	client *http.Client
}

func NewFrontendServer(cfg FrontendConfig) *FrontendServer {
	if cfg.MetadataIdentityURL == "" {
		cfg.MetadataIdentityURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity"
	}
	return &FrontendServer{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *FrontendServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealthz)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/checkout", s.handleCheckout)
	mux.HandleFunc("/receipts/", s.handleReceipt)
	mux.HandleFunc("/admin", s.handleAdmin)
	mux.HandleFunc("/support/chat", s.handleSupportChat)
	mux.HandleFunc("/", s.handleIndex)
	return mux
}

func (s *FrontendServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "frontend",
	})
}

func (s *FrontendServer) fetchMetadataToken(inReq *http.Request, audience string) (string, error) {
	u, err := url.Parse(s.cfg.MetadataIdentityURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()

	metaReq, err := http.NewRequestWithContext(inReq.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	metaReq.Header.Set("Metadata-Flavor", "Google")
	resp, err := s.client.Do(metaReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata server returned status %d", resp.StatusCode)
	}
	tokBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(tokBytes)), nil
}

func (s *FrontendServer) attachOutboundAuth(outReq *http.Request, inReq *http.Request, audience string) error {
	if s.cfg.UseOIDCAuth {
		tok, err := s.fetchMetadataToken(inReq, audience)
		if err != nil {
			return err
		}
		outReq.Header.Set("Authorization", "Bearer "+tok)
		return nil
	}

	inAuth := inReq.Header.Get("Authorization")
	if strings.Contains(inAuth, "SIGNATURE_REMOVED_BY_GOOGLE") {
		if tok, err := s.fetchMetadataToken(inReq, audience); err == nil && tok != "" {
			outReq.Header.Set("Authorization", "Bearer "+tok)
			return nil
		}
	}
	if inAuth != "" {
		outReq.Header.Set("Authorization", inAuth)
	}
	return nil
}

func (s *FrontendServer) handleCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.BackendURL == "" {
		http.Error(w, `{"error":"BACKEND_URL not configured"}`, http.StatusServiceUnavailable)
		return
	}
	target := strings.TrimRight(s.cfg.BackendURL, "/") + "/api/orders"
	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	outReq.Header.Set("Content-Type", "application/json")
	if err := s.attachOutboundAuth(outReq, r, strings.TrimRight(s.cfg.BackendURL, "/")); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	resp, err := s.client.Do(outReq)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *FrontendServer) handleReceipt(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/receipts/")
	if id == "" || s.cfg.BackendURL == "" {
		http.Error(w, `{"error":"missing receipt id or BACKEND_URL"}`, http.StatusBadRequest)
		return
	}
	target := strings.TrimRight(s.cfg.BackendURL, "/") + "/api/receipts/" + url.PathEscape(id)
	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if err := s.attachOutboundAuth(outReq, r, strings.TrimRight(s.cfg.BackendURL, "/")); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	resp, err := s.client.Do(outReq)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *FrontendServer) handleAdmin(w http.ResponseWriter, r *http.Request) {
	iapEmail := r.Header.Get("X-Goog-Authenticated-User-Email")
	iapEmail = strings.TrimPrefix(iapEmail, "accounts.google.com:")
	if iapEmail == "" {
		iapEmail = "anonymous (IAP not active)"
	}

	ordersJSON := "[]"
	if s.cfg.BackendURL != "" {
		target := strings.TrimRight(s.cfg.BackendURL, "/") + "/api/orders"
		outReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
		if err == nil {
			_ = s.attachOutboundAuth(outReq, r, strings.TrimRight(s.cfg.BackendURL, "/"))
			if resp, err := s.client.Do(outReq); err == nil {
				if b, err := io.ReadAll(resp.Body); err == nil {
					ordersJSON = string(b)
				}
				_ = resp.Body.Close()
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>TelP Secure Shop — Admin Backoffice</title></head>
<body>
  <h1>TelP Secure Shop — Administration Portal</h1>
  <p>Authenticated IAP Identity: <strong>%s</strong></p>
  <h2>Recent Orders</h2>
  <pre>%s</pre>
</body>
</html>`, html.EscapeString(iapEmail), html.EscapeString(ordersJSON))
}

func (s *FrontendServer) handleSupportChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.AIAssistantURL == "" {
		http.Error(w, `{"error":"AI_ASSISTANT_URL not configured"}`, http.StatusServiceUnavailable)
		return
	}
	target := strings.TrimRight(s.cfg.AIAssistantURL, "/") + "/api/chat"
	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	outReq.Header.Set("Content-Type", "application/json")
	if err := s.attachOutboundAuth(outReq, r, strings.TrimRight(s.cfg.AIAssistantURL, "/")); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	resp, err := s.client.Do(outReq)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *FrontendServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query().Get("q")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>TelP Secure Shop</title>
  <style>
    body { font-family: system-ui, -apple-system, sans-serif; margin: 2rem auto; max-width: 860px; padding: 0 1rem; color: #1f2937; background: #f8fafc; }
    .card { background: #fff; border: 1px solid #e2e8f0; border-radius: 8px; padding: 1.25rem; margin-bottom: 1.25rem; box-shadow: 0 1px 2px rgba(0,0,0,0.04); }
    button { background: #2563eb; color: #fff; border: none; border-radius: 6px; padding: 0.5rem 1rem; cursor: pointer; font-weight: 600; }
    button:hover { background: #1d4ed8; }
    input, select { padding: 0.45rem 0.6rem; border: 1px solid #cbd5e1; border-radius: 6px; width: 100%%; box-sizing: border-box; margin-bottom: 0.75rem; }
    pre { background: #0f172a; color: #e2e8f0; padding: 0.85rem; border-radius: 6px; overflow-x: auto; font-size: 0.85rem; }
    .nav a { margin-right: 1rem; color: #2563eb; text-decoration: none; font-weight: 600; }
  </style>
</head>
<body>
  <h1>TelP Secure Shop — DMZ Storefront (%s)</h1>
  <div class="nav" style="margin-bottom:1rem;">
    <a href="/">Storefront</a>
    <a href="/admin">Admin Backoffice (/admin)</a>
    <a href="/health">Frontend Health (/health)</a>
  </div>
  <div class="card">
    <p><strong>Search Filter:</strong> <code>%s</code> | <strong>OIDC Service-to-Service Auth:</strong> <code>%v</code></p>
    <form method="GET" action="/">
      <input type="text" name="q" placeholder="Filter hardware security catalog (e.g. yubikey)..." value="%s" />
      <button type="submit">Search Catalog</button>
    </form>
  </div>
  <div class="card">
    <h2>Place Order (POST /checkout &rarr; Backend Firestore + GCS Receipt)</h2>
    <label>Customer Name</label>
    <input id="cust_name" value="Etudiant Test" />
    <label>Customer Email</label>
    <input id="cust_email" value="etudiant.test@gcp.telecom-paris.fr" />
    <label>Item &amp; Price (EUR)</label>
    <select id="cust_item">
      <option value="yubikey-5c-nfc|49.90">YubiKey 5C NFC — 49.90 EUR</option>
      <option value="titan-security-key|35.00">Google Titan Security Key — 35.00 EUR</option>
      <option value="hsm-backup-module|199.00">Cloud HSM Backup Token — 199.00 EUR</option>
    </select>
    <button type="button" onclick="placeOrder()">Checkout &amp; Sign Receipt</button>
    <pre id="checkout_out">Click "Checkout &amp; Sign Receipt" to test frontend &rarr; backend &rarr; Firestore + GCS.</pre>
  </div>
  <div class="card">
    <h2>Verify Signed Receipt (GET /receipts/{id})</h2>
    <input id="receipt_id" placeholder="ord-..." />
    <button type="button" onclick="checkReceipt()">Verify Receipt HMAC</button>
    <pre id="receipt_out">Enter an order_id above to verify its GCS receipt &amp; HMAC-SHA256 signature.</pre>
  </div>
  <script>
    async function placeOrder() {
      const out = document.getElementById('checkout_out');
      out.textContent = 'Submitting order to /checkout...';
      const [item, price] = document.getElementById('cust_item').value.split('|');
      try {
        const res = await fetch('/checkout', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            customer_name: document.getElementById('cust_name').value,
            customer_email: document.getElementById('cust_email').value,
            amount_eur: parseFloat(price),
            items: [item]
          })
        });
        const txt = await res.text();
        out.textContent = 'HTTP ' + res.status + '\n' + txt;
        try {
          const data = JSON.parse(txt);
          if (data.order_id) document.getElementById('receipt_id').value = data.order_id;
        } catch (_) {}
      } catch (e) {
        out.textContent = 'Error: ' + e;
      }
    }
    async function checkReceipt() {
      const id = document.getElementById('receipt_id').value.trim();
      const out = document.getElementById('receipt_out');
      if (!id) { out.textContent = 'Please enter an order_id.'; return; }
      out.textContent = 'Fetching /receipts/' + id + '...';
      try {
        const res = await fetch('/receipts/' + encodeURIComponent(id));
        out.textContent = 'HTTP ' + res.status + '\n' + await res.text();
      } catch (e) {
        out.textContent = 'Error: ' + e;
      }
    }
  </script>
</body>
</html>`, html.EscapeString(s.cfg.ProjectID), html.EscapeString(query), s.cfg.UseOIDCAuth, html.EscapeString(query))
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
	useOIDC := strings.EqualFold(os.Getenv("USE_OIDC_AUTH"), "true")

	srv := NewFrontendServer(FrontendConfig{
		ProjectID:      projectID,
		BackendURL:     os.Getenv("BACKEND_URL"),
		AIAssistantURL: os.Getenv("AI_ASSISTANT_URL"),
		UseOIDCAuth:    useOIDC,
	})
	log.Printf("TelP Secure Shop frontend listening on :%s (project=%s, use_oidc=%v)", port, projectID, useOIDC)
	if err := http.ListenAndServe(":"+port, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
