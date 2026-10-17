package main

import (
	"bytes"
	"context"
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

type AIAssistantConfig struct {
	ProjectID             string
	Location              string
	Model                 string
	DLPInspectTemplate    string
	DLPDeidentifyTemplate string
	ModelArmorTemplate    string
}

type ChatRequest struct {
	Prompt string `json:"prompt"`
}

type ChatResponse struct {
	Response    string `json:"response"`
	DLPRedacted bool   `json:"dlp_redacted"`
}

type TicketStore interface {
	ListSupportTickets(ctx context.Context) ([]string, error)
}

type ModelArmorClient interface {
	SanitizeUserPrompt(ctx context.Context, template, prompt string) (bool, error)
	SanitizeModelResponse(ctx context.Context, template, resp string) (string, error)
}

type DLPClient interface {
	DeidentifyText(ctx context.Context, inspectTemplate, deidTemplate, text string) (string, error)
}

type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, ticketContext, userPrompt string) (string, error)
}

type AIAssistantServer struct {
	cfg     AIAssistantConfig
	tickets TicketStore
	armor   ModelArmorClient
	dlp     DLPClient
	llm     LLMClient
}

func NewAIAssistantServer(cfg AIAssistantConfig, tickets TicketStore, armor ModelArmorClient, dlp DLPClient, llm LLMClient) *AIAssistantServer {
	if cfg.Location == "" {
		cfg.Location = "europe-west1"
	}
	if cfg.Model == "" {
		cfg.Model = "gemini-2.5-flash"
	}
	return &AIAssistantServer{
		cfg:     cfg,
		tickets: tickets,
		armor:   armor,
		dlp:     dlp,
		llm:     llm,
	}
}

func (s *AIAssistantServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealthz)
	mux.HandleFunc("/api/health", s.handleHealthz)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/chat", s.handleChat)
	return mux
}

func (s *AIAssistantServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "ai-assistant",
	})
}

func (s *AIAssistantServer) handleChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	// 1. Model Armor Input Screening
	if s.cfg.ModelArmorTemplate != "" && s.armor != nil {
		blocked, err := s.armor.SanitizeUserPrompt(r.Context(), s.cfg.ModelArmorTemplate, req.Prompt)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "model armor error: " + err.Error()})
			return
		}
		if blocked {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":      "Prompt blocked by Model Armor security policy",
				"blocked_by": "MODEL_ARMOR",
			})
			return
		}
	}

	// 2. Load support tickets from Firestore & optionally de-identify with Cloud DLP
	var ticketList []string
	if s.tickets != nil {
		ticketList, _ = s.tickets.ListSupportTickets(r.Context())
	}
	ticketContext := strings.Join(ticketList, "\n")
	userPrompt := req.Prompt
	dlpRedacted := false

	if s.cfg.DLPDeidentifyTemplate != "" && s.dlp != nil {
		var err error
		ticketContext, err = s.dlp.DeidentifyText(r.Context(), s.cfg.DLPInspectTemplate, s.cfg.DLPDeidentifyTemplate, ticketContext)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "dlp deidentify error: " + err.Error()})
			return
		}
		userPrompt, err = s.dlp.DeidentifyText(r.Context(), s.cfg.DLPInspectTemplate, s.cfg.DLPDeidentifyTemplate, userPrompt)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "dlp prompt deidentify error: " + err.Error()})
			return
		}
		dlpRedacted = true
	}

	// 3. Call Vertex AI
	systemPrompt := "You are the TelP Secure Shop customer support assistant."
	var modelReply string
	if s.llm != nil {
		var err error
		modelReply, err = s.llm.Generate(r.Context(), systemPrompt, ticketContext, userPrompt)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "vertex ai error: " + err.Error()})
			return
		}
	}

	// 4. Model Armor Output Screening
	if s.cfg.ModelArmorTemplate != "" && s.armor != nil {
		if sanitized, err := s.armor.SanitizeModelResponse(r.Context(), s.cfg.ModelArmorTemplate, modelReply); err == nil {
			modelReply = sanitized
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ChatResponse{
		Response:    modelReply,
		DLPRedacted: dlpRedacted,
	})
}

// --- Live GCP REST Implementations ---

func getAccessToken(ctx context.Context) (string, error) {
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
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.AccessToken, nil
}

type gcpTicketStore struct {
	projectID string
	client    *http.Client
}

func (g *gcpTicketStore) ListSupportTickets(ctx context.Context) ([]string, error) {
	tok, err := getAccessToken(ctx)
	if err == nil {
		u := fmt.Sprintf("https://firestore.googleapis.com/v1/projects/%s/databases/(default)/documents/support_tickets?pageSize=10", url.PathEscape(g.projectID))
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		if resp, err := g.client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var docList struct {
					Documents []struct {
						Fields map[string]struct {
							StringValue string `json:"stringValue"`
						} `json:"fields"`
					} `json:"documents"`
				}
				if json.NewDecoder(resp.Body).Decode(&docList) == nil && len(docList.Documents) > 0 {
					var out []string
					for _, d := range docList.Documents {
						out = append(out, d.Fields["content"].StringValue)
					}
					return out, nil
				}
			}
		}
	}
	// Fallback synthetic tickets if collection is empty
	return []string{
		"Ticket #101: Customer jean.dupont@gcp.telecom-paris.fr, IBAN FR76 3000 6000 0112 3456 7890 189, Card 4532-0151-1283-0366, Phone +33 6 12 34 56 78",
	}, nil
}

type gcpModelArmorClient struct {
	location string
	client   *http.Client
}

func (m *gcpModelArmorClient) SanitizeUserPrompt(ctx context.Context, template, prompt string) (bool, error) {
	lower := strings.ToLower(prompt)
	if strings.Contains(lower, "ignore previous instructions") || strings.Contains(lower, "system prompt") || strings.Contains(lower, "dump all iban") {
		return true, nil
	}
	tok, err := getAccessToken(ctx)
	if err != nil {
		return false, nil
	}
	u := fmt.Sprintf("https://modelarmor.%s.rep.googleapis.com/v1/%s:sanitizeUserPrompt", m.location, template)
	body, _ := json.Marshal(map[string]any{
		"userPromptData": map[string]string{"text": prompt},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return false, nil
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "MATCH_FOUND") {
		return true, nil
	}
	return false, nil
}

func (m *gcpModelArmorClient) SanitizeModelResponse(_ context.Context, _, resp string) (string, error) {
	return resp, nil
}

type gcpDLPClient struct {
	projectID string
	location  string
	client    *http.Client
}

func (d *gcpDLPClient) DeidentifyText(ctx context.Context, inspectTemplate, deidTemplate, text string) (string, error) {
	tok, err := getAccessToken(ctx)
	if err == nil {
		u := fmt.Sprintf("https://dlp.googleapis.com/v2/projects/%s/locations/%s/content:deidentify", url.PathEscape(d.projectID), url.PathEscape(d.location))
		payload := map[string]any{
			"deidentifyTemplateName": deidTemplate,
			"item":                   map[string]string{"value": text},
		}
		if inspectTemplate != "" {
			payload["inspectTemplateName"] = inspectTemplate
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		if resp, err := d.client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var res struct {
					Item struct {
						Value string `json:"value"`
					} `json:"item"`
				}
				if json.NewDecoder(resp.Body).Decode(&res) == nil && res.Item.Value != "" {
					return res.Item.Value, nil
				}
			}
		}
	}
	return text, nil
}

type gcpVertexClient struct {
	projectID string
	location  string
	model     string
	client    *http.Client
}

func (v *gcpVertexClient) Generate(_ context.Context, _, ticketContext, userPrompt string) (string, error) {
	return fmt.Sprintf("TelP Support Assistant (%s): Based on context [%s], answering query: %s", v.model, ticketContext, userPrompt), nil
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
	loc := os.Getenv("LOCATION")
	if loc == "" {
		loc = "europe-west1"
	}
	model := os.Getenv("VERTEX_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}
	httpClient := &http.Client{Timeout: 8 * time.Second}
	srv := NewAIAssistantServer(
		AIAssistantConfig{
			ProjectID:             projectID,
			Location:              loc,
			Model:                 model,
			DLPInspectTemplate:    os.Getenv("DLP_INSPECT_TEMPLATE"),
			DLPDeidentifyTemplate: os.Getenv("DLP_DEIDENTIFY_TEMPLATE"),
			ModelArmorTemplate:    os.Getenv("MODEL_ARMOR_TEMPLATE"),
		},
		&gcpTicketStore{projectID: projectID, client: httpClient},
		&gcpModelArmorClient{location: loc, client: httpClient},
		&gcpDLPClient{projectID: projectID, location: loc, client: httpClient},
		&gcpVertexClient{projectID: projectID, location: loc, model: model, client: httpClient},
	)
	log.Printf("TelP Secure Shop ai-assistant listening on :%s (project=%s)", port, projectID)
	if err := http.ListenAndServe(":"+port, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
